package core

import (
	"math"
	"testing"
)

// W'bal 恒等式（规格：docs/ALGORITHMS.md §3，Skiba 2012 分段模型——
// τ = 546·exp(−0.01·(CP − P̄below)) + 316；超 CP 线性扣减、低于 CP 按 τ 指数回充）。
//
// 真值必须**独立实现**分段式。旧版这里的 naive() 照抄了「前向积分 + 并发衰减」，
// 与实现同错，所以测不出「小幅持续超 CP 永不耗竭」——见
// TestWBalSustainedSmallSurgeDepletes。

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

	t.Run("恒定超功率：与分段式独立真值对拍", func(t *testing.T) {
		// 独立真值（Skiba 分段模型字面语义）：超 CP 每步减 above·Δt 并钳 0；不恢复。
		p := make([]float64, 600)
		for i := range p {
			p[i] = cpTest + 100
		}
		const above = 100.0
		bal := WBal(p, cpTest, wprimeTest, 316, 1)
		naive := func(t int) float64 { // t 为该步之后
			v := wprimeTest - above*float64(t+1)
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

	// 回归：持续小幅超 CP（ΔP=20W）必须线性耗竭。
	// 旧的前向积分式在这里只降到 13936J（W'=20kJ），因为衰减同时施加在超 CP 段，
	// 储备趋近 W' − τ·ΔP 就停住，永远到不了 0。
	t.Run("小幅持续超 CP 线性耗竭（积分式 bug 回归）", func(t *testing.T) {
		p := make([]float64, 1300)
		for i := range p {
			p[i] = cpTest + 20 // 20 kJ / 20 W = 1000 s 应耗尽
		}
		bal := WBal(p, cpTest, wprimeTest, 316, 1)
		if bal[500] > 10500 { // 理论 10000
			t.Fatalf("t=500 应为 ~10000, 得到 %.0f（衰减被错误地施加在超 CP 段）", bal[500])
		}
		if bal[1000] != 0 || bal[1299] != 0 {
			t.Fatalf("t=1000 起应已耗竭，得到 %.0f / %.0f", bal[1000], bal[1299])
		}
	})

	// 恢复段必须按 e^(−Δt/τ) 走（分段式的解析解），不是「按剩余比例」的线性近似。
	t.Run("恢复段解析解", func(t *testing.T) {
		const tau = 316.0
		p := make([]float64, 200)
		for i := 0; i < 100; i++ {
			p[i] = cpTest + 100 // 先挖掉 10000J
		}
		for i := 100; i < 200; i++ {
			p[i] = cpTest - 50 // 再恢复 100 秒
		}
		bal := WBal(p, cpTest, wprimeTest, tau, 1)
		want := wprimeTest - 10000*math.Exp(-100/tau)
		if math.Abs(bal[199]-want) > 1e-6 {
			t.Fatalf("恢复 100s 期望 %.1f, 得到 %.1f", want, bal[199])
		}
	})

	// 停表/缺测（p=0）不改变储备：不能把没记录的段落当成恢复白送 W'。
	t.Run("p=0 不恢复也不消耗", func(t *testing.T) {
		p := make([]float64, 300)
		for i := 0; i < 100; i++ {
			p[i] = cpTest + 100 // 消耗 10000J
		}
		for i := 100; i < 300; i++ {
			p[i] = 0
		}
		bal := WBal(p, cpTest, wprimeTest, 316, 1)
		if bal[299] != bal[99] {
			t.Fatalf("p=0 段不应改变储备: %.1f → %.1f", bal[99], bal[299])
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

func TestFitCPWPrime(t *testing.T) {
	// 合成 P = 250 + 20000/t（独立真值），验证线性化拟合回收 CP/W'
	durations := []float64{120, 180, 240, 300, 420, 600}
	pts := make([]EffortPoint, 0, len(durations))
	for _, sec := range durations {
		pts = append(pts, EffortPoint{Seconds: sec, DistanceM: 250 + 20000/sec})
	}
	cp, wprime, quality := FitCPWPrime(pts)
	if math.Abs(cp-250) > 1e-6 {
		t.Fatalf("期望 CP=250, 得到 %v", cp)
	}
	if math.Abs(wprime-20000) > 1e-6 {
		t.Fatalf("期望 W'=20000, 得到 %v", wprime)
	}
	if quality.R2 < 0.999 || quality.N != len(durations) {
		t.Fatalf("期望高质量拟合, 得到 %+v", quality)
	}
}
