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
	N      int     `json:"n"` // 参与拟合的成绩数
	R2     float64 `json:"r2"`
	RMSE   float64 `json:"rmse"`
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

func fitBanister(dailyLoads map[string]float64, races []RacePoint, tau1, tau2, lambdaDays float64) BanisterModel {
	out := BanisterModel{Tau1: tau1, Tau2: tau2, Lambda: lambdaDays, N: len(races), loads: dailyLoads}
	if len(races) < 3 || tau1 <= 0 || tau2 <= 0 {
		out.RMSE = -1 // 约定：-1 表示样本不足
		return out
	}

	// 设计矩阵：每行 [1, A_j, -B_j]，另带该点权重 w
	type row struct{ a, b, y, w float64 }
	rows := make([]row, 0, len(races))
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
	for _, p := range ps {
		w := 1.0
		if lambdaDays > 0 {
			w = math.Exp(-(lastDay - p.d) / lambdaDays)
		}
		rows = append(rows, row{
			a: accumulated(dailyLoads, p.d, tau1),
			b: accumulated(dailyLoads, p.d, tau2),
			y: p.perf,
			w: w,
		})
	}
	if len(rows) < 3 {
		out.N = len(rows)
		out.RMSE = -1
		return out
	}

	// 正规方程 (XᵀWX)·θ = XᵀWy —— 实现上给每行同乘 √w，展开就是同一个式子
	var ata [3][3]float64
	var aty [3]float64
	for _, r := range rows {
		sw := math.Sqrt(r.w)
		x := [3]float64{sw, sw * r.a, -(sw * r.b)}
		yw := sw * r.y
		for i := 0; i < 3; i++ {
			aty[i] += x[i] * yw
			for j := 0; j < 3; j++ {
				ata[i][j] += x[i] * x[j]
			}
		}
	}
	theta, ok := solve3(ata, aty)
	if !ok {
		out.RMSE = -1
		return out
	}

	// 设计矩阵第三列已是 -B，所以 theta[2] 就是 k2（模型里以减法出现）
	out.P0, out.K1, out.K2 = theta[0], theta[1], theta[2]
	out.N = len(rows)

	// 质量指标（加权）。λ=0 时所有 w=1，逐值退化成原来的算术口径，
	// 所以金标准 dump 的 R²/RMSE 不会因为这次改动而变。
	var sumW, ssRes, ssTot float64
	for _, r := range rows {
		sumW += r.w
	}
	mean := 0.0
	for _, r := range rows {
		mean += r.w * r.y
	}
	mean /= sumW
	for _, r := range rows {
		pred := out.P0 + out.K1*r.a - out.K2*r.b
		d := r.y - pred
		ssRes += r.w * d * d
		d2 := r.y - mean
		ssTot += r.w * d2 * d2
	}
	out.RMSE = math.Sqrt(ssRes / sumW)
	if ssTot > 0 {
		out.R2 = 1 - ssRes/ssTot
	}
	// 3 个参数 + n 个观测 → 自由度 n-3。n ≤ 3 时拟合必然穿过所有点，
	// R² 恒为 1，没有统计意义。
	out.Overfit = len(rows) <= 3
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
