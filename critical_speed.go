package core

import "math"

// 临界速度模型（票 08）。
// 规格：docs/goldencheetah-integration/04 §1/§3.3 的跑步对应物——
// 2 参数模型 D(t) = D' + CS·t（D'：米；CS：m/s），最小二乘线性回归拟合；
// D'bal 微分式（GC WPrime 微分式的速度域对应）：
//
//	v < CS: bal += (CS−v)·(D'−bal)/D'   （按剩余比例恢复）
//	v ≥ CS: bal += (CS−v)               （线性消耗，钳到 ≥0）

// EffortPoint 一个"全力段"观察点：t 秒内完成的最大距离。
type EffortPoint struct {
	Seconds   float64 `json:"seconds"`
	DistanceM float64 `json:"distanceM"`
}

// FitQuality 拟合质量。
type FitQuality struct {
	N     int     `json:"n"`     // 样本点数
	R2    float64 `json:"r2"`    // 决定系数
	RMSEM float64 `json:"rmseM"` // 残差均方根（米）
}

// FitCriticalSpeed 用最小二乘拟合 D(t) = D' + CS·t。
// 样本 <2 或时长全相同（分母 0）返回零值与低质量。
func FitCriticalSpeed(pts []EffortPoint) (cs, dprime float64, quality FitQuality) {
	quality.N = len(pts)
	if len(pts) < 2 {
		return 0, 0, quality
	}
	var n, sumT, sumD, sumTT, sumTD float64
	for _, p := range pts {
		n++
		sumT += p.Seconds
		sumD += p.DistanceM
		sumTT += p.Seconds * p.Seconds
		sumTD += p.Seconds * p.DistanceM
	}
	denom := n*sumTT - sumT*sumT
	if denom == 0 {
		return 0, 0, quality
	}
	cs = (n*sumTD - sumT*sumD) / denom
	dprime = (sumD - cs*sumT) / n

	// R² 与 RMSE
	var ssRes, ssTot float64
	meanD := sumD / n
	for _, p := range pts {
		pred := dprime + cs*p.Seconds
		ssRes += (p.DistanceM - pred) * (p.DistanceM - pred)
		ssTot += (p.DistanceM - meanD) * (p.DistanceM - meanD)
	}
	if ssTot > 0 {
		quality.R2 = 1 - ssRes/ssTot
	}
	quality.RMSEM = math.Sqrt(ssRes / n)
	return cs, dprime, quality
}

// BestEfforts 提取多个时长的"最大距离"观察点（速度 km/h 输入）。
// 对每个目标时长 t：滑窗求和取最大 → (t, maxDistance)。O(n·k) 滑窗实现。
func BestEfforts(speedsKph []float64, intervalSecs int, durationsSecs []int) []EffortPoint {
	out := make([]EffortPoint, 0, len(durationsSecs))
	for _, want := range durationsSecs {
		k := want / intervalSecs
		if k <= 0 || k > len(speedsKph) {
			continue
		}
		// 距离 = Σ speed(m/s)·interval；用滑动窗口
		best, win := 0.0, 0.0
		for i, v := range speedsKph {
			win += v / 3.6 * float64(intervalSecs)
			if i >= k {
				win -= speedsKph[i-k] / 3.6 * float64(intervalSecs)
			}
			if i >= k-1 && win > best {
				best = win
			}
		}
		out = append(out, EffortPoint{Seconds: float64(want), DistanceM: best})
	}
	return out
}

// DBal 计算 D'balance 序列（速度 km/h 输入）。GC WPrime 微分式的速度域对应：
// v≥CS 线性消耗、v<CS 按剩余比例恢复；钳到 [0, D']。
func DBal(speedsKph []float64, csMS, dprime float64, intervalSecs int) []float64 {
	out := make([]float64, len(speedsKph))
	if csMS <= 0 || dprime <= 0 || intervalSecs <= 0 {
		return out
	}
	bal := dprime
	for i, vKph := range speedsKph {
		v := vKph / 3.6
		if v >= csMS {
			bal += (csMS - v) * float64(intervalSecs) // 负增量
		} else {
			bal += (csMS - v) * (dprime - bal) / dprime * float64(intervalSecs)
		}
		if bal < 0 {
			bal = 0
		}
		if bal > dprime {
			bal = dprime
		}
		out[i] = bal
	}
	return out
}

// ActivityEfforts 是单次活动在各目标时长上的最佳努力（与 durations 一一对应，
// DistanceM 为 0 表示该活动时长不够）。
type ActivityEfforts struct {
	ActivityID uint
	Points     []EffortPoint
}

// SelectFitPoints 从各活动的全力段里挑出用于拟合的点集，**保证每次活动最多贡献 1 个点**。
//
// 为什么需要：CS/D' 拟合最容易出的错是「同一次匀速跑在 600/720/900 秒上都最好」——
// 等于用同一条直线喂了多个点，R² 必然接近 1，但 D' 会小到没有生理意义
// （实测 D'=71m，正常 150–300m），最终 D'bal 曲线全程满值、是条死线。
//
// 策略：对每个目标时长，在所有活动里挑最快的一条；若该活动已经被别的时长占用，
// 就顺延到下一条更慢但未被占用的活动。返回选中的点集与参与的活动数。
func SelectFitPoints(perActivity []ActivityEfforts, durations []int) ([]EffortPoint, int) {
	used := make([]bool, len(perActivity))
	out := make([]EffortPoint, 0, len(durations))

	for i, want := range durations {
		// 收集该时长的所有候选（活动下标 + 距离），按距离降序
		type cand struct {
			idx  int
			dist float64
		}
		cands := make([]cand, 0, len(perActivity))
		for ai := range perActivity {
			pts := perActivity[ai].Points
			if i >= len(pts) || pts[i].DistanceM <= 0 {
				continue
			}
			cands = append(cands, cand{idx: ai, dist: pts[i].DistanceM})
		}
		// 插入排序（候选数 = 活动数，规模很小）
		for a := 1; a < len(cands); a++ {
			for b := a; b > 0 && cands[b].dist > cands[b-1].dist; b-- {
				cands[b], cands[b-1] = cands[b-1], cands[b]
			}
		}
		for _, c := range cands {
			if used[c.idx] {
				continue
			}
			used[c.idx] = true
			out = append(out, EffortPoint{Seconds: float64(want), DistanceM: c.dist})
			break
		}
	}

	nActs := 0
	for _, u := range used {
		if u {
			nActs++
		}
	}
	return out, nActs
}

// CP3Result 是三参数模型的拟合结果。
type CP3Result struct {
	// CS 长期可持续的速度/功率（同 2 参数模型的 CS/CP）。
	CS float64 `json:"cs"`
	// DPrime 储备距离/做功（同 2 参数模型的 D'/W'）。
	DPrime float64 `json:"dprime"`
	// Tau 饱和时间常数（秒）：越小说明短时能力越快耗尽。
	Tau float64 `json:"tau"`
	// Peak 瞬时极限（t→0）：CS + D'/τ。功率域即 Pmax。
	Peak float64 `json:"peak"`
	// PeakReliable 为 false 表示 τ 没被数据约束住（典型：最短全力段仍长于 τ，
	// 饱和早已完成 → τ 塌到下界，Peak 会被算成荒谬的大值）。此时**不要**展示 Peak。
	PeakReliable bool       `json:"peakReliable"`
	Quality      FitQuality `json:"quality"`
}

// FitCP3 拟合三参数模型 D(t) = D'·(1 − e^(−t/τ)) + CS·t。
//
// 与 2 参数版 D(t) = D' + CS·t 的区别：2 参数假定「储备在 t=0 就能全部动用」，
// 会高估极短时间的表现；3 参数用一个指数饱和项描述「储备的动用需要时间」，
// 因此能给出可信的瞬时极限 Peak = CS + D'/τ（功率域就是 Pmax）。
//
// 实现是纯 Go 的 Levenberg-Marquardt（数值微分求雅可比 + 高斯消元解 3×3），
// 不引入任何第三方数值库。初值取自 2 参数线性拟合 + τ0=300s。
func FitCP3(pts []EffortPoint) CP3Result {
	out := CP3Result{Quality: FitQuality{N: len(pts)}}
	if len(pts) < 3 {
		out.Quality.RMSEM = -1 // 约定：-1 表示样本不足
		return out
	}

	// 初值：2 参数线性拟合
	cs, dprime, linQ := FitCriticalSpeed(pts)
	if cs <= 0 {
		out.Quality.RMSEM = -1
		return out
	}
	theta := []float64{cs, dprime, 300}

	model := func(t, cs, dp, tau float64) float64 {
		return dp*(1-math.Exp(-t/tau)) + cs*t
	}
	sse := func(th []float64) float64 {
		s := 0.0
		for _, p := range pts {
			r := model(p.Seconds, th[0], th[1], th[2]) - p.DistanceM
			s += r * r
		}
		return s
	}

	lambda := 1e-3
	cur := sse(theta)
	for iter := 0; iter < 200; iter++ {
		// 数值雅可比（3 列）
		const h = 1e-6
		J := make([][3]float64, len(pts))
		for i, p := range pts {
			base := model(p.Seconds, theta[0], theta[1], theta[2])
			for k := 0; k < 3; k++ {
				th := append([]float64{}, theta...)
				th[k] += h * math.Max(1, math.Abs(theta[k]))
				J[i][k] = (model(p.Seconds, th[0], th[1], th[2]) - base) /
					(h * math.Max(1, math.Abs(theta[k])))
			}
		}
		// A = JᵀJ + λ·diag, b = -Jᵀr
		var A [3][3]float64
		var b [3]float64
		for i, p := range pts {
			r := model(p.Seconds, theta[0], theta[1], theta[2]) - p.DistanceM
			for a := 0; a < 3; a++ {
				b[a] -= J[i][a] * r
				for c := 0; c < 3; c++ {
					A[a][c] += J[i][a] * J[i][c]
				}
			}
		}
		for a := 0; a < 3; a++ {
			A[a][a] *= 1 + lambda
		}
		delta, ok := solve3(A, b)
		if !ok {
			break
		}
		cand := []float64{theta[0] + delta[0], theta[1] + delta[1], theta[2] + delta[2]}
		// 物理约束：CS>0、D'>0、τ∈[10, 3000]
		// τ 下界取 30s：跑步的短时饱和常数通常 30–120s，低于 30s 属于数值退化
		if cand[0] <= 0 || cand[1] <= 0 || cand[2] < 30 || cand[2] > 3000 {
			lambda *= 5
			continue
		}
		next := sse(cand)
		if next < cur {
			theta, cur = cand, next
			lambda = math.Max(lambda/3, 1e-9)
			if math.Abs(delta[0]) < 1e-9 && math.Abs(delta[1]) < 1e-7 && math.Abs(delta[2]) < 1e-6 {
				break
			}
		} else {
			lambda *= 5
			if lambda > 1e10 {
				break
			}
		}
	}

	if cur <= 0 || math.IsInf(cur, 0) || math.IsNaN(cur) {
		out.Quality.RMSEM = -1
		return out
	}
	out.CS = theta[0]
	out.DPrime = theta[1]
	out.Tau = theta[2]
	out.Peak = theta[0] + theta[1]/theta[2]
	// τ 若贴在下界（30s），说明它根本没被数据约束住 —— 最短的全力段仍远长于 τ，
	// 指数项早已饱和。这时 Peak 是外推出来的，不可信。
	tauPinned := theta[2] <= 30.0001
	// 瞬时极限还不应超过长期可持续能力的 2.5 倍（跑步经验上限，功率域同理）
	out.PeakReliable = !tauPinned && out.Peak <= theta[0]*2.5
	out.Quality.RMSEM = math.Sqrt(cur / float64(len(pts)))

	// R²
	mean := 0.0
	for _, p := range pts {
		mean += p.DistanceM
	}
	mean /= float64(len(pts))
	ssTot := 0.0
	for _, p := range pts {
		d := p.DistanceM - mean
		ssTot += d * d
	}
	if ssTot > 0 {
		out.Quality.R2 = 1 - cur/ssTot
	}
	_ = linQ
	return out
}

// solve3 解 3×3 线性方程组（高斯消元 + 部分主元）。奇异时返回 false。
func solve3(A [3][3]float64, b [3]float64) ([3]float64, bool) {
	var out [3]float64
	m := [3][4]float64{
		{A[0][0], A[0][1], A[0][2], b[0]},
		{A[1][0], A[1][1], A[1][2], b[1]},
		{A[2][0], A[2][1], A[2][2], b[2]},
	}
	for col := 0; col < 3; col++ {
		piv := col
		for r := col + 1; r < 3; r++ {
			if math.Abs(m[r][col]) > math.Abs(m[piv][col]) {
				piv = r
			}
		}
		if math.Abs(m[piv][col]) < 1e-12 {
			return out, false
		}
		m[col], m[piv] = m[piv], m[col]
		for r := 0; r < 3; r++ {
			if r == col {
				continue
			}
			f := m[r][col] / m[col][col]
			for c := col; c < 4; c++ {
				m[r][c] -= f * m[col][c]
			}
		}
	}
	for i := 0; i < 3; i++ {
		out[i] = m[i][3] / m[i][i]
	}
	return out, true
}
