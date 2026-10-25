package core

import (
	"math"
	"testing"
)

// W'bal 恒等式（规格：docs/goldencheetah-integration/04 §3.2 积分式——
// τ = 546·exp(−0.01·(CP − P̄below)) + 316；前向积分 I += exp(t/τ)·(P−CP)+，
// 消耗 = exp(−t/τ)·I；W'bal = W' − 消耗）。

const cpTest = 250.0
const wprimeTest = 20000.0

func TestTauFromData(t *testing.T) {
	t.Run("公式（Skiba）", func(t *testing.T) {
		// P̄below = 150 → τ = 546·exp(−0.01·100)+316 = 546·0.3679+316 ≈ 516.9
		got := TauFromData(cpTest, 150)
		want := 546*math.Exp(-0.01*(cpTest-150)) + 316
		if math.Abs(got-want) > 1e-9 || math.Abs(got-516.9) > 1 {
			t.Fatalf("期望 ≈%v, 得到 %v", want, got)
		}
	})
	t.Run("P̄below=CP → τ=862（546+316，GC 公式原样）", func(t *testing.T) {
		got := TauFromData(cpTest, cpTest)
		if math.Abs(got-862) > 1e-9 {
			t.Fatalf("期望 862, 得到 %v", got)
		}
	})
}

func TestWBal(t *testing.T) {
	t.Run("全程功率 ≤ CP：W'bal 恒等于 W'", func(t *testing.T) {
		p := make([]float64, 600)
		for i := range p {
			p[i] = 200 // < CP
		}
		bal := WBal(p, cpTest, wprimeTest, 316, 1)
		for i, v := range bal {
			if math.Abs(v-wprimeTest) > 1e-9 {
				t.Fatalf("期望恒等于 %v, 在 %d 得到 %v", wprimeTest, i, v)
			}
		}
	})

	t.Run("恒定超功率：与朴素定义对拍（独立真值）", func(t *testing.T) {
		// 朴素定义（04 文档原文语义）：bal(t) = W' − Σ_{u≤t} e^((t−u)/τ)·above(u)·Δt，钳 [0,W']
		p := make([]float64, 600)
		for i := range p {
			p[i] = cpTest + 100
		}
		const tau = 316.0
		const above = 100.0
		bal := WBal(p, cpTest, wprimeTest, tau, 1)
		naive := func(t int) float64 {
			sum := 0.0
			for u := 0; u <= t; u++ {
				sum += math.Exp(-float64(t-u)/tau) * above
			}
			v := wprimeTest - sum
			if v < 0 {
				v = 0
			}
			return v
		}
		for _, tSec := range []int{60, 300, 599} {
			if math.Abs(bal[tSec]-naive(tSec)) > 1e-6 {
				t.Fatalf("t=%d 期望 %v, 得到 %v", tSec, naive(tSec), bal[tSec])
			}
		}
		for i, v := range bal {
			if v < -1e-9 || v > wprimeTest+1e-9 {
				t.Fatalf("钳位失败 @%d: %v", i, v)
			}
		}
	})

	t.Run("钳位到 0", func(t *testing.T) {
		p := make([]float64, 1200)
		for i := range p {
			p[i] = cpTest + 200 // 消耗极快
		}
		bal := WBal(p, cpTest, wprimeTest, 316, 1)
		for i, v := range bal {
			if v < -1e-9 {
				t.Fatalf("W'bal 为负 @%d: %v", i, v)
			}
		}
	})

	t.Run("CP 上下交替：消耗-恢复-消耗", func(t *testing.T) {
		// 60s@CP+150 → 60s@CP−100 → 60s@CP+150
		p := make([]float64, 180)
		for i := 0; i < 60; i++ {
			p[i] = cpTest + 150
		}
		for i := 60; i < 120; i++ {
			p[i] = cpTest - 100
		}
		for i := 120; i < 180; i++ {
			p[i] = cpTest + 150
		}
		bal := WBal(p, cpTest, wprimeTest, 316, 1)
		if bal[59] >= wprimeTest {
			t.Fatal("超功率段应下降")
		}
		if bal[119] <= bal[59] {
			t.Fatalf("恢复段应回升: %v → %v", bal[59], bal[119])
		}
		if bal[179] >= bal[59] {
			t.Fatal("再消耗段应再度下降")
		}
	})
}
