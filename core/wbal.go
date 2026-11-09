package core

import "math"

// W'bal（票 09）：Skiba 2012 分段模型（docs/ALGORITHMS.md §3）。
//
//	τ = 546·exp(−0.01·(CP − P̄below)) + 316      P̄below = CP 以下功率均值
//	P > CP:  W'bal −= (P − CP)·Δt，钳到 [0, W']  # 超临界段线性扣减，无回充
//	P ≤ CP:  W'bal  = W' − (W' − W'bal)·e^(−Δt/τ) # 恢复段按 τ 指数回充
//
// 出处：Skiba et al., MSSE 2012（"store and pothole" 是文中比喻）。
//
// 为什么不是「前向积分」式：把 I(t) += e^(t/τ)·(P−CP)+ 再乘 e^(−t/τ) 得到的是
// dW'bal/dt = −(P−CP) + (W'−W'bal)/τ —— 超临界段也带一个回充项，储备只能趋近
// W' − τ·(P−CP)，**持续小幅超 CP 时永不耗竭**（实测 ΔP=20W/τ=316/W'=20kJ：
// 1000s 后仍剩 13936J，应为 0）。GC 的 GC_WBALFORM=int 用这个式子只是省 CPU 的
// 近似，其 WPrime.cpp 的微分实现（minForCP）与 Skiba 原文都是分段式；本仓 DBal
// 也是分段式，功率版与速度版必须同模型。

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
	dt := float64(intervalSecs)
	bal := wprime
	for i, p := range power {
		switch {
		case p > cp:
			// 超临界：线性扣减，无回充
			bal -= (p - cp) * dt
			if bal < 0 {
				bal = 0
			}
		case p > 0:
			// 恢复段：W'bal = W' − (W' − W'bal)·e^(−Δt/τ)
			bal = wprime - (wprime-bal)*math.Exp(-dt/tau)
			if bal > wprime {
				bal = wprime
			}
		}
		// p ≤ 0（停表 / 缺测）：储备不变。把停表段当恢复会让长时间停车
		// 白送满 W'，而这类样本往往只是没记录，不是真的在滑行。
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
// （GC CP2Model 线性化：P = W'·(1/t) + CP——对自变量 x=1/t 而言斜率=W'、截距=CP。）
// 出处：GC CP2Model（docs/goldencheetah-integration/04 §1 表）。
func FitCPWPrime(pts []EffortPoint) (cp, wprime float64, quality FitQuality) {
	inverse := make([]EffortPoint, len(pts))
	for i, p := range pts {
		if p.Seconds <= 0 {
			return 0, 0, FitQuality{N: len(pts)}
		}
		inverse[i] = EffortPoint{Seconds: 1 / p.Seconds, DistanceM: p.DistanceM}
	}
	// FitCriticalSpeed 对 (t,D) 拟合 D=D'+CS·t：slope=CS、intercept=D'。
	// 代入 x=1/t 后：slope↔W'、intercept↔CP（Round 7 修正此前返回值对调的笔误）。
	slope, intercept, quality := FitCriticalSpeed(inverse)
	quality.N = len(pts)
	return intercept, slope, quality
}
