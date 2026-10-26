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
