package core

import (
	"math"
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
	N    int     `json:"n"`    // 参与拟合的成绩数
	R2   float64 `json:"r2"`
	RMSE float64 `json:"rmse"`
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

// FitBanisterWithTau 指定 τ 的版本（便于测试与后续开放参数）。
func FitBanisterWithTau(dailyLoads map[string]float64, races []RacePoint, tau1, tau2 float64) BanisterModel {
	out := BanisterModel{Tau1: tau1, Tau2: tau2, N: len(races), loads: dailyLoads}
	if len(races) < 3 || tau1 <= 0 || tau2 <= 0 {
		out.RMSE = -1 // 约定：-1 表示样本不足
		return out
	}

	// 设计矩阵：每行 [1, A_j, -B_j]
	type row struct{ a, b, y float64 }
	rows := make([]row, 0, len(races))
	for _, rp := range races {
		d, ok := dayIndex(rp.Date)
		if !ok || rp.Performance <= 0 {
			continue
		}
		rows = append(rows, row{
			a: accumulated(dailyLoads, d, tau1),
			b: accumulated(dailyLoads, d, tau2),
			y: rp.Performance,
		})
	}
	if len(rows) < 3 {
		out.N = len(rows)
		out.RMSE = -1
		return out
	}

	// 正规方程 AᵀA·θ = Aᵀy
	var ata [3][3]float64
	var aty [3]float64
	for _, r := range rows {
		x := [3]float64{1, r.a, -r.b}
		for i := 0; i < 3; i++ {
			aty[i] += x[i] * r.y
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

	// 质量指标
	var ssRes, ssTot float64
	mean := 0.0
	for _, r := range rows {
		mean += r.y
	}
	mean /= float64(len(rows))
	for _, r := range rows {
		pred := out.P0 + out.K1*r.a - out.K2*r.b
		d := r.y - pred
		ssRes += d * d
		d2 := r.y - mean
		ssTot += d2 * d2
	}
	out.RMSE = math.Sqrt(ssRes / float64(len(rows)))
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

// timeFromIndex 把「自 1970-01-01 起的天数」还原成时间（测试与预测共用）。
func timeFromIndex(days float64) time.Time {
	return time.Unix(int64(days*86400), 0).UTC()
}
