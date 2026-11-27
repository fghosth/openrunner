package core

import (
	"math"
	"sort"
	"time"
)

// Banister 刺激-响应模型（GC Banister.cpp 的等价实现）。
//
//	表现 p(t) = p0 + k1·Σ w_i·e^(−(t−i)/τ1) − k2·Σ w_i·e^(−(t−i)/τ2)
//
// 直观解释：训练同时产生「变强」（正效应，衰减慢，τ1≈45 天）和「疲劳」
// （负效应，衰减快，τ2≈15 天）。当前的比赛表现 = 基础能力 + 累积正效应 − 累积疲劳。
//
// 实现选择：GC 用 lmfit 同时拟合 p0/k1/k2/τ1/τ2 五个参数（非线性）。
// 这里**固定 τ1/τ2**（学界常用 45/15），此时模型对 p0/k1/k2 是**线性的**，
// 可以直接解 3×3 正规方程 —— 无需非线性优化、无迭代不收敛风险，也不需要外部库。
// τ 的拟合留作后续（需要更多成绩点才有意义）。

// RacePoint 一条比赛成绩换算出的表现观测点。
type RacePoint struct {
	// Date 比赛日期（YYYY-MM-DD）
	Date string
	// Performance 归一化后的表现值（越大越好）：参考距离下的等价速度 m/s
	Performance float64
}

// BanisterModel 拟合结果与预测能力。
type BanisterModel struct {
	P0   float64 `json:"p0"`   // 基础表现（无训练时的水平）
	K1   float64 `json:"k1"`   // 正效应系数
	K2   float64 `json:"k2"`   // 负效应系数
	Tau1 float64 `json:"tau1"` // 正效应时间常数（天）
	Tau2 float64 `json:"tau2"` // 负效应时间常数（天）
	// Lambda 近期权重尺度（天）：w = exp(−(t_last − t_i)/Lambda)。
	// 0 表示不加权（普通最小二乘），也是金标准 dump 走的口径。
	Lambda float64 `json:"lambda"`
	// R2Adj 调整 R²：1 − (1−R²)·(n−1)/(n−p)。加参数总能抬高 R²，
	// 只有调整 R² 才不会被这种「进步」骗到 —— 5 参数拟合 8 个点时尤其必要。
	R2Adj float64 `json:"r2Adj"`
	// Params 自由参数个数：固定 τ 是 3（p0/k1/k2），拟合 τ 是 5
	Params int `json:"params"`
	// TauFitted 为 true 表示 τ1/τ2 来自网格搜索，不是固定的 45/15
	TauFitted bool `json:"tauFitted"`
	// TauAtBoundary 最优落在搜索边界上 → τ **不可辨识**（SSE 在边界内单调），
	// 返回的 τ 只是「边界内最优」，不能当生理常数展示给用户。
	TauAtBoundary bool    `json:"tauAtBoundary"`
	N             int     `json:"n"` // 参与拟合的成绩数
	R2            float64 `json:"r2"`
	RMSE          float64 `json:"rmse"`
	// Overfit 表示成绩点数不足以支撑 3 个参数（n ≤ 3 时自由度 ≤ 0，
	// R² 必然接近 1，属于过拟合，**不能**据此判断模型好坏）。
	Overfit bool `json:"overfit"`
	// Loads / 用于预测时回放累积项
	loads map[string]float64 `json:"-"`
}

// RaceRiegelEquivalent 用 Riegel 公式把一次成绩换算到参考距离的等价用时（秒）。
//
//	t_ref = t · (d_ref / d)^1.06
//
// 指数 1.06 是 Riegel 的经验值（跑步项目通用），用来消除「5K 和全马不能直接比」的问题。
func RaceRiegelEquivalent(distanceM, timeSecs, refDistanceM float64) float64 {
	if distanceM <= 0 || timeSecs <= 0 || refDistanceM <= 0 {
		return 0
	}
	return timeSecs * math.Pow(refDistanceM/distanceM, 1.06)
}

// RacePerformance 把成绩换算成「参考距离下的等价速度（m/s）」—— 越大越好。
func RacePerformance(distanceM, timeSecs, refDistanceM float64) float64 {
	eq := RaceRiegelEquivalent(distanceM, timeSecs, refDistanceM)
	if eq <= 0 {
		return 0
	}
	return refDistanceM / eq
}

// dayIndex 把 YYYY-MM-DD 转成自 1970-01-01 起的天数（用于指数衰减的差值）。
func dayIndex(date string) (float64, bool) {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return 0, false
	}
	return t.Sub(time.Unix(0, 0).UTC()).Hours() / 24, true
}

// accumulated 计算某日之前的指数加权累积负荷（严格早于该日）。
func accumulated(daily map[string]float64, day float64, tau float64) float64 {
	sum := 0.0
	for d, w := range daily {
		idx, ok := dayIndex(d)
		if !ok || idx >= day || w <= 0 {
			continue
		}
		sum += w * math.Exp(-(day-idx)/tau)
	}
	return sum
}

// FitBanister 固定 τ1=45、τ2=15，用最小二乘解 p0/k1/k2。
// 少于 3 个成绩点时返回 N=0（无法唯一确定 3 个参数）。
func FitBanister(dailyLoads map[string]float64, races []RacePoint) BanisterModel {
	return FitBanisterWithTau(dailyLoads, races, 45, 15)
}

// FitBanisterWithTau 指定 τ 的**无权重**版本（金标准 dump 走这条，λ=0）。
func FitBanisterWithTau(dailyLoads map[string]float64, races []RacePoint, tau1, tau2 float64) BanisterModel {
	return fitBanister(dailyLoads, races, tau1, tau2, 0)
}

// FitBanisterWeighted 指定 τ 与近期权重尺度 lambdaDays 的版本。
//
// lambdaDays > 0 时每条成绩按 w = exp(−(t_last − t_i)/lambdaDays) 加权，
// t_last 是成绩集合里最新的一天。这是 Banister 原式之外的 openrunner 扩展，
// 用来解决「近期变强了但拟合跟不上」：不加权时八个月前的慢成绩和上周的
// 快成绩同权，最小二乘只能取中间，于是系统性低估当前状态。
//
// 权重锚在 t_last 而不是「今天」：加权最小二乘对权重的整体缩放不敏感，
// 两者数学等价，但锚 t_last 的解不随查询日期漂移，可复现。
func FitBanisterWeighted(dailyLoads map[string]float64, races []RacePoint, tau1, tau2, lambdaDays float64) BanisterModel {
	return fitBanister(dailyLoads, races, tau1, tau2, lambdaDays)
}

// banisterPrep 把与 τ 无关的部分算一次：每个成绩点对应的「负荷年龄 / 负荷值」列表、
// 观测值与权重。τ 搜索的每个格点因此只需重算指数和，不再扫负荷 map、不再解析日期。
type banisterPrep struct {
	ages [][]float64
	ws   [][]float64
	perf []float64
	sw   []float64 // √w_i
	sumW float64
	n    int
}

func prepBanister(dailyLoads map[string]float64, races []RacePoint, lambdaDays float64) banisterPrep {
	type pending struct {
		d    float64
		perf float64
	}
	ps := make([]pending, 0, len(races))
	var lastDay float64
	for _, rp := range races {
		d, ok := dayIndex(rp.Date)
		if !ok || rp.Performance <= 0 {
			continue
		}
		ps = append(ps, pending{d: d, perf: rp.Performance})
		if d > lastDay {
			lastDay = d
		}
	}
	type lw struct{ d, w float64 }
	lws := make([]lw, 0, len(dailyLoads))
	for ds, w := range dailyLoads {
		if w <= 0 {
			continue
		}
		if d, ok := dayIndex(ds); ok {
			lws = append(lws, lw{d, w})
		}
	}
	p := banisterPrep{n: len(ps)}
	for _, v := range ps {
		var ag, ww []float64
		for _, l := range lws {
			if l.d >= v.d {
				continue
			}
			ag = append(ag, v.d-l.d)
			ww = append(ww, l.w)
		}
		p.ages = append(p.ages, ag)
		p.ws = append(p.ws, ww)
		p.perf = append(p.perf, v.perf)
		s := 1.0
		if lambdaDays > 0 {
			s = math.Exp(-(lastDay - v.d) / (2.0 * lambdaDays))
		}
		p.sw = append(p.sw, s)
		p.sumW += s * s
	}
	return p
}

// banisterCore 是给定 (τ1, τ2) 的加权最小二乘解与拟合质量。
type banisterCore struct {
	p0, k1, k2 float64
	sse        float64
	r2         float64
	rmse       float64
	ok         bool
}

// solve 解 (p0,k1,k2) 的加权最小二乘。正规方程是 (XᵀWX)·θ = XᵀWy，
// 实现上给每行同乘 √w，展开就是同一个式子。
func (p banisterPrep) solve(tau1, tau2 float64) banisterCore {
	var out banisterCore
	if p.n < 3 || tau1 <= 0 || tau2 <= 0 {
		return out
	}
	as := make([]float64, p.n)
	bs := make([]float64, p.n)
	var ata [3][3]float64
	var aty [3]float64
	for i := 0; i < p.n; i++ {
		var a, b float64
		ages, wgt := p.ages[i], p.ws[i]
		for j, age := range ages {
			a += wgt[j] * math.Exp(-age/tau1)
			b += wgt[j] * math.Exp(-age/tau2)
		}
		as[i], bs[i] = a, b
		s := p.sw[i]
		x := [3]float64{s, s * a, -(s * b)}
		y := s * p.perf[i]
		for k := 0; k < 3; k++ {
			aty[k] += x[k] * y
			for m := 0; m < 3; m++ {
				ata[k][m] += x[k] * x[m]
			}
		}
	}
	theta, ok := solve3(ata, aty)
	if !ok {
		return out
	}
	out.p0, out.k1, out.k2 = theta[0], theta[1], theta[2]
	mean := 0.0
	for i := 0; i < p.n; i++ {
		mean += p.sw[i] * p.sw[i] * p.perf[i]
	}
	mean /= p.sumW
	var ssTot float64
	for i := 0; i < p.n; i++ {
		pred := out.p0 + out.k1*as[i] - out.k2*bs[i]
		d := p.perf[i] - pred
		out.sse += p.sw[i] * p.sw[i] * d * d
		d2 := p.perf[i] - mean
		ssTot += p.sw[i] * p.sw[i] * d2 * d2
	}
	out.rmse = math.Sqrt(out.sse / p.sumW)
	if ssTot > 0 {
		out.r2 = 1 - out.sse/ssTot
	}
	out.ok = true
	return out
}

func fitBanister(dailyLoads map[string]float64, races []RacePoint, tau1, tau2, lambdaDays float64) BanisterModel {
	out := BanisterModel{Tau1: tau1, Tau2: tau2, Lambda: lambdaDays, N: len(races), loads: dailyLoads}
	if len(races) < 3 || tau1 <= 0 || tau2 <= 0 {
		out.RMSE = -1 // 约定：-1 表示样本不足
		return out
	}
	p := prepBanister(dailyLoads, races, lambdaDays)
	if p.n < 3 {
		out.N = p.n
		out.RMSE = -1
		return out
	}
	c := p.solve(tau1, tau2)
	if !c.ok {
		out.N = p.n
		out.RMSE = -1
		return out
	}
	// 设计矩阵第三列已是 -B，所以 c.k2 就是 k2（模型里以减法出现）
	out.P0, out.K1, out.K2 = c.p0, c.k1, c.k2
	out.N = p.n
	// 质量指标（加权）。λ=0 时所有 w=1，逐值退化成原来的算术口径，
	// 所以金标准 dump 的 R²/RMSE 不会因为这次改动而变。
	out.RMSE = c.rmse
	out.R2 = c.r2
	// 3 个参数 + n 个观测 → 自由度 n-3。n ≤ 3 时拟合必然穿过所有点，
	// R² 恒为 1，没有统计意义。
	out.Overfit = p.n <= 3
	out.Params = 3
	out.R2Adj = adjustedR2(out.R2, out.N, out.Params)
	return out
}

// Predict 用模型预测某日的表现（参考距离下的等价速度 m/s）。
func (m BanisterModel) Predict(date string) float64 {
	d, ok := dayIndex(date)
	if !ok || m.RMSE < 0 {
		return 0
	}
	a := accumulated(m.loads, d, m.Tau1)
	b := accumulated(m.loads, d, m.Tau2)
	return m.P0 + m.K1*a - m.K2*b
}

// FormBias 描述「最近若干条成绩」与拟合曲线之间的系统性偏差。
//
// 模型只有 3 个自由参数、τ 又固定，遇到近期状态跃升时会整体低估或高估。
// 与其把明显偏离实际的预测直接端给用户，不如把这个偏差量出来标在界面上，
// 并提供一个锚在最近实际成绩上的替代预测。
type FormBias struct {
	// N 参与判断的成绩条数（实际取到的，可能小于请求的窗口）
	N int `json:"n"`
	// MeanSecs 平均（模型等价用时 − 实际用时），正数 = 模型比实际慢
	MeanSecs float64 `json:"meanSecs"`
	// MeanFrac 同上，除以实际用时的相对值
	MeanFrac float64 `json:"meanFrac"`
	// Direction "modelSlow"（模型偏慢）| "modelFast"（模型偏快）| "none"
	Direction string `json:"direction"`
	// AnchorPerf 偏差成立时：最近 N 条**实际**等价表现的平均值（m/s），
	// 上层用它替代模型值做距离外推。Direction 为 none 时是 0。
	AnchorPerf float64  `json:"anchorPerf"`
	Dates      []string `json:"dates"`
}

// DetectFormBias 检查按日期排序的最后 n 条成绩是否**一致**偏离拟合曲线。
//
// 判成系统性需要两个条件同时成立：
//  1. 这 n 条残差全部同号（有快有慢说明是噪声，不是系统偏差）；
//  2. 平均相对偏差绝对值 ≥ minFrac。
//
// 条件 1 是关键：只看平均值的话，一条快一条慢会互相抵消，把噪声读成偏差。
func DetectFormBias(m BanisterModel, races []RacePoint, refDistanceM float64, n int, minFrac float64) FormBias {
	out := FormBias{Direction: "none"}
	if refDistanceM <= 0 || n < 2 || m.RMSE < 0 {
		return out
	}
	valid := make([]RacePoint, 0, len(races))
	for _, rp := range races {
		if _, ok := dayIndex(rp.Date); ok && rp.Performance > 0 {
			valid = append(valid, rp)
		}
	}
	sort.Slice(valid, func(i, j int) bool { return valid[i].Date < valid[j].Date })
	if len(valid) > n {
		valid = valid[len(valid)-n:]
	}
	if len(valid) < 2 {
		return out
	}
	var sumFrac, sumSecs, sumPerf float64
	pos, neg := 0, 0
	for _, rp := range valid {
		pred := m.Predict(rp.Date)
		if pred <= 0 {
			return FormBias{Direction: "none", N: len(valid)}
		}
		tAct := refDistanceM / rp.Performance
		tPred := refDistanceM / pred
		frac := (tPred - tAct) / tAct
		if frac > 0 {
			pos++
		} else if frac < 0 {
			neg++
		}
		sumFrac += frac
		sumSecs += tPred - tAct
		sumPerf += rp.Performance
		out.Dates = append(out.Dates, rp.Date)
	}
	k := float64(len(valid))
	out.N = len(valid)
	out.MeanFrac = sumFrac / k
	out.MeanSecs = sumSecs / k
	// 同号 + 超阈值才算系统性偏差
	if (pos == len(valid) || neg == len(valid)) && math.Abs(out.MeanFrac) >= minFrac {
		if out.MeanFrac > 0 {
			out.Direction = "modelSlow"
		} else {
			out.Direction = "modelFast"
		}
		out.AnchorPerf = sumPerf / k
	}
	return out
}

// timeFromIndex 把「自 1970-01-01 起的天数」还原成时间（测试与预测共用）。
func timeFromIndex(days float64) time.Time {
	return time.Unix(int64(days*86400), 0).UTC()
}

// adjustedR2 调整决定系数：1 − (1−R²)·(n−1)/(n−p)。n ≤ p 时无意义，返回 NaN。
func adjustedR2(r2 float64, n, params int) float64 {
	if n <= params {
		return math.NaN()
	}
	return 1 - (1-r2)*float64(n-1)/float64(n-params)
}

// τ 搜索边界。Banister 1975 / Res 1989 给出的常用值约 τ1≈45–60、τ2≈10–20 天；
// 边界放得比文献宽，是为了让「数据想跑到边界外」这件事本身成为可读出的信号
// （TauAtBoundary），而不是被边界悄悄截断。
const (
	BanisterTau1Min   = 20.0
	BanisterTau1Max   = 180.0
	BanisterTau2Min   = 5.0
	BanisterTau2Max   = 60.0
	banisterTauSteps  = 24
	banisterTauRefine = 2
	// BanisterMinRacesForTauFit 拟合 5 个参数至少需要的成绩条数。
	// n ≤ 5 时自由度 ≤ 0，调整 R² 无意义，退回固定 τ。
	BanisterMinRacesForTauFit = 6
)

// geomSpace 含端点的等比数列。与参照 goldenlib._geomspace 逐字一致 ——
// 两侧网格必须落在同一点上，否则「Go 与参照一致」无从谈起。
func geomSpace(a, b float64, n int) []float64 {
	if n <= 1 || a <= 0 || b <= 0 {
		return []float64{a}
	}
	r := math.Pow(b/a, 1.0/float64(n-1))
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		out[i] = a * math.Pow(r, float64(i))
	}
	return out
}

// FitBanisterFitTau 在边界内搜索 (τ1, τ2)，每个格点上用加权最小二乘精确解
// (p0, k1, k2) —— 可分离最小二乘（variable projection）。
//
// GoldenCheetah 用 lmfit 跑 5 参数非线性拟合。这里改用网格 + 精确线性解，
// 是为了让参照与 Go 能**逐位一致**地对照：LM 有路径依赖，两份独立实现很容易
// 落在不同局部解上，那种「一致」是假的。
//
// 目标函数用 RMSE 而不是 SSE：sumW 只取决于 λ 与成绩日期、与 τ 无关，
// 所以 RMSE = √(SSE/sumW) 与 SSE 单调等价，比较结果相同。
//
// 返回的模型 TauFitted=true；若最优落在边界上则 TauAtBoundary=true，
// 表示 τ 不可辨识（SSE 在边界内单调），此时 τ 只是「边界内最优」。
// 实测：状态短期台阶式上升的跑者，τ1 会一路撞上界 —— 因为数据支持的是
// 「持续性水平抬升」，不是可衰减的卷积项。
func FitBanisterFitTau(dailyLoads map[string]float64, races []RacePoint, lambdaDays float64) BanisterModel {
	fallback := fitBanister(dailyLoads, races, 45, 15, lambdaDays)
	if len(races) < BanisterMinRacesForTauFit {
		return fallback
	}
	p := prepBanister(dailyLoads, races, lambdaDays)
	bestTau1, bestTau2 := 45.0, 15.0
	// 固定 τ 的 RMSE 作为下界起点：搜索只接受严格更优的格点，
	// 保证「拟合 τ」永远不会让拟合质量变差
	bestScore := fallback.RMSE
	if bestScore < 0 {
		return fallback
	}
	var bestCore banisterCore
	c1 := geomSpace(BanisterTau1Min, BanisterTau1Max, banisterTauSteps)
	c2 := geomSpace(BanisterTau2Min, BanisterTau2Max, banisterTauSteps)
	for pass := 0; pass <= banisterTauRefine; pass++ {
		for _, t1 := range c1 {
			for _, t2 := range c2 {
				if t2 >= t1 {
					continue // 疲劳必须比体能衰减得快，这是模型前提
				}
				// 目标函数用 RMSE 而非 SSE：sumW 只取决于 λ 与成绩日期、与 τ 无关，
				// 所以 RMSE = √(SSE/sumW) 与 SSE 单调等价，比较结果相同
				c := p.solve(t1, t2)
				if !c.ok {
					continue
				}
				if c.rmse < bestScore {
					bestScore = c.rmse
					bestCore = c
					bestTau1, bestTau2 = t1, t2
				}
			}
		}
		if pass == banisterTauRefine {
			break
		}
		// 细化窗口：围绕当前最优各扩 1.6 倍，并夹回边界内
		n1 := geomSpace(math.Max(BanisterTau1Min, bestTau1/1.6), math.Min(BanisterTau1Max, bestTau1*1.6), banisterTauSteps)
		n2 := geomSpace(math.Max(BanisterTau2Min, bestTau2/1.6), math.Min(BanisterTau2Max, bestTau2*1.6), banisterTauSteps)
		c1, c2 = n1, n2
	}
	if !bestCore.ok {
		return fallback // 没有任何格点优于固定 45/15
	}
	best := fallback
	best.P0, best.K1, best.K2 = bestCore.p0, bestCore.k1, bestCore.k2
	best.RMSE = bestCore.rmse
	best.R2 = bestCore.r2
	best.Tau1, best.Tau2 = bestTau1, bestTau2
	best.TauFitted = true
	eps := 1e-9
	best.TauAtBoundary = math.Abs(bestTau1-BanisterTau1Min) < eps || math.Abs(bestTau1-BanisterTau1Max) < eps ||
		math.Abs(bestTau2-BanisterTau2Min) < eps || math.Abs(bestTau2-BanisterTau2Max) < eps
	best.Params = 5
	best.R2Adj = adjustedR2(best.R2, best.N, best.Params)
	return best
}
