package core

import "math"

// W'bal（票 09）：Skiba 2012 积分式（docs/goldencheetah-integration/04 §3.2）。
//
//	τ = 546·exp(−0.01·(CP − P̄below)) + 316      P̄below = CP 以下功率均值
//	I(t)      += exp(t/τ)·(P−CP)+               # 前向累加（只对超 CP 部分）
//	W'reexp(t) = exp(−t/τ)·I(t)                  # 指数衰减的"已消耗总量"
//	W'bal(t)   = W' − W'reexp(t)，钳 [0, W']
//
// 出处：Skiba et al., MSSE 2012（04 文档 §3.2）；前向积分实现与 04 文档工程注记一致
// （免回看历史，CPU 省一半；数值上等价于按秒重算积分）。

// TauFromData Skiba τ 公式：P̄below 为 CP 以下功率均值（无低功率数据时 = CP → τ=316）。
func TauFromData(cp, avgPowerBelowCP float64) float64 {
	return 546*math.Exp(-0.01*(cp-avgPowerBelowCP)) + 316
}

// WBal 计算 W'balance 序列（功率 W 输入）。
func WBal(power []float64, cp, wprime, tau float64, intervalSecs int) []float64 {
	out := make([]float64, len(power))
	if cp <= 0 || wprime <= 0 || tau <= 0 || intervalSecs <= 0 || len(power) == 0 {
		return out
	}
	I := 0.0
	for i, p := range power {
		t := float64(i * intervalSecs)
		above := p - cp
		if above > 0 {
			I += math.Exp(t/tau) * above * float64(intervalSecs)
		}
		bal := wprime - math.Exp(-t/tau)*I
		if bal < 0 {
			bal = 0
		}
		if bal > wprime {
			bal = wprime
		}
		out[i] = bal
	}
	return out
}

// AvgPowerBelowCP 功率流中低于 CP 部分的均值（τ 公式输入）；无数据返回 0。
func AvgPowerBelowCP(power []float64, cp float64) float64 {
	sum, n := 0.0, 0
	for _, p := range power {
		if p > 0 && p < cp {
			sum += p
			n++
		}
	}
	if n == 0 {
		return cp // 无低于 CP 数据 → 公式退化为 τ=316
	}
	return sum / float64(n)
}

// FitCPWPrime 跑步功率的 2 参数模型拟合：P(t) = CP + W'/t 对 (1/t, P) 线性回归。
// （GC CP2Model 的线性化：P = CP + W'·(1/t)，斜率=W'、截距=CP。）
func FitCPWPrime(pts []EffortPoint) (cp, wprime float64, quality FitQuality) {
	inverse := make([]EffortPoint, len(pts))
	for i, p := range pts {
		inverse[i] = EffortPoint{Seconds: 1 / p.Seconds, DistanceM: p.DistanceM}
	}
	// 复用 OLS：P = intercept + slope·(1/t) → intercept=CP, slope=W'
	cp, wprime, quality = FitCriticalSpeed(inverse)
	quality.N = len(pts)
	// R² 重算（自变量是 1/t，SS_Tot 仍用 P 均值——FitCriticalSpeed 内部一致）
	return cp, wprime, quality
}
