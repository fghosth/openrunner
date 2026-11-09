package core

import (
	"math"
	"testing"
)

// CS/D' 恒等式（规格：docs/goldencheetah-integration/04 §1/§3.3 跑步对应物；
// 2 参数模型 D(t) = D' + CS·t，最小二乘拟合；D'bal 微分式）。

func makeEfforts(cs, dprime float64, durations []float64) []EffortPoint {
	// 用已知 CS/D' 生成"全力段"点（距离-时长对）
	pts := make([]EffortPoint, 0, len(durations))
	for _, t := range durations {
		pts = append(pts, EffortPoint{Seconds: t, DistanceM: dprime + cs*t})
	}
	return pts
}

func TestFitCriticalSpeed(t *testing.T) {
	t.Run("合成数据精确回收 CS/D'", func(t *testing.T) {
		pts := makeEfforts(4.5, 180, []float64{120, 180, 300, 420, 600})
		cs, dprime, quality := FitCriticalSpeed(pts)
		if math.Abs(cs-4.5) > 1e-6 {
			t.Fatalf("期望 CS=4.5, 得到 %v", cs)
		}
		if math.Abs(dprime-180) > 1e-6 {
			t.Fatalf("期望 D'=180, 得到 %v", dprime)
		}
		if quality.R2 < 0.999 {
			t.Fatalf("期望 R²≈1, 得到 %v", quality.R2)
		}
	})

	t.Run("带噪声数据近似回收（拟合稳健性）", func(t *testing.T) {
		pts := []EffortPoint{
			{Seconds: 120, DistanceM: 180 + 4.5*120 + 5}, // +5m 噪声
			{Seconds: 180, DistanceM: 180 + 4.5*180 - 4},
			{Seconds: 300, DistanceM: 180 + 4.5*300 + 3},
			{Seconds: 420, DistanceM: 180 + 4.5*420 - 2},
			{Seconds: 600, DistanceM: 180 + 4.5*600 + 1},
		}
		cs, dprime, _ := FitCriticalSpeed(pts)
		if math.Abs(cs-4.5) > 0.05 {
			t.Fatalf("期望 CS≈4.5±0.05, 得到 %v", cs)
		}
		if math.Abs(dprime-180) > 15 {
			t.Fatalf("期望 D'≈180±15, 得到 %v", dprime)
		}
	})

	t.Run("点数不足 → 零值+低质量", func(t *testing.T) {
		cs, dprime, quality := FitCriticalSpeed(makeEfforts(4, 100, []float64{300}))
		if cs != 0 || dprime != 0 || quality.N != 1 || quality.R2 != 0 {
			t.Fatalf("期望零值+低质量, 得到 CS=%v D'=%v %+v", cs, dprime, quality)
		}
	})
}

func TestBestEfforts(t *testing.T) {
	t.Run("多时长最佳配速点提取", func(t *testing.T) {
		// 60s@5m/s + 60s@4m/s + 60s@6m/s：best[60]=300m(5m/s), best[120]=540m(4.5), best[180]=900
		speeds := make([]float64, 180)
		for i := 0; i < 60; i++ {
			speeds[i] = 18 // 5 m/s = 18 km/h
		}
		for i := 60; i < 120; i++ {
			speeds[i] = 14.4 // 4 m/s
		}
		for i := 120; i < 180; i++ {
			speeds[i] = 21.6 // 6 m/s
		}
		pts := BestEfforts(speeds, 1, []int{60, 120, 180})
		if len(pts) != 3 {
			t.Fatalf("期望 3 点, 得到 %d", len(pts))
		}
		// best 60s = max window：6m/s 段 → 360m
		if math.Abs(pts[0].DistanceM-360) > 1e-9 || pts[0].Seconds != 60 {
			t.Fatalf("期望 60s→360m, 得到 %+v", pts[0])
		}
		// 120s 窗最优 = 末两段 4m/s+6m/s = 240+360 = 600m（>首两段 300+240=540）
		if math.Abs(pts[1].DistanceM-600) > 1e-9 {
			t.Fatalf("期望 120s→600m, 得到 %+v", pts[1])
		}
		// 180s 全程 = 300+240+360 = 900m
		if math.Abs(pts[2].DistanceM-900) > 1e-9 {
			t.Fatalf("期望 180s→900m, 得到 %+v", pts[2])
		}
	})
}

func TestDBal(t *testing.T) {
	const cs, dprime = 4.0, 200.0 // m/s, m

	t.Run("低于 CS 恒速：D'bal 单调回升", func(t *testing.T) {
		speeds := make([]float64, 300)
		for i := range speeds {
			speeds[i] = 14.4 // 4 m/s = 14.4 km/h < CS
		}
		bal := DBal(speeds, cs, dprime, 1)
		for i := 1; i < len(speeds); i++ {
			if bal[i] < bal[i-1]-1e-9 {
				t.Fatalf("期望单调回升, 在 %d 下降 %v→%v", i, bal[i-1], bal[i])
			}
		}
		if bal[len(speeds)-1] > dprime+1e-9 {
			t.Fatal("D'bal 不得超过 D'")
		}
	})

	t.Run("全程超 CS：累计消耗不超 D'", func(t *testing.T) {
		speeds := make([]float64, 60)
		for i := range speeds {
			speeds[i] = 21.6 // 6 m/s > CS
		}
		bal := DBal(speeds, cs, dprime, 1)
		if bal[len(speeds)-1] < -1e-9 {
			t.Fatalf("D'bal 不应为负(钳到0): %v", bal[len(speeds)-1])
		}
		// 消耗速率 = 6−4 = 2 m/s → 60s 耗 120m → 末值 = 200−120 = 80
		if math.Abs(bal[len(speeds)-1]-80) > 1e-6 {
			t.Fatalf("期望 80, 得到 %v", bal[len(speeds)-1])
		}
	})

	t.Run("先耗后充：恢复按剩余比例", func(t *testing.T) {
		// 30s@6m/s 耗 60m（余 140）→ 30s@2m/s 恢复
		speeds := make([]float64, 60)
		for i := 0; i < 30; i++ {
			speeds[i] = 21.6
		}
		for i := 30; i < 60; i++ {
			speeds[i] = 7.2 // 2 m/s
		}
		bal := DBal(speeds, cs, dprime, 1)
		if bal[59] <= bal[30] {
			t.Fatalf("恢复段应回升: %v → %v", bal[30], bal[59])
		}
		if bal[59] > dprime {
			t.Fatal("不得超过 D'")
		}
	})
}

func TestSelectFitPoints(t *testing.T) {
	durs := []int{180, 300, 420, 600, 720, 900}

	t.Run("单次活动不得在多个时长上重复占位", func(t *testing.T) {
		// 活动 1 在所有时长上都最快（典型的单次匀速跑）
		dominant := ActivityEfforts{ActivityID: 1}
		for _, d := range durs {
			dominant.Points = append(dominant.Points, EffortPoint{
				Seconds: float64(d), DistanceM: float64(d) * 4.0, // 4 m/s
			})
		}
		// 活动 2/3 只覆盖部分时长且更慢
		a2 := ActivityEfforts{ActivityID: 2, Points: []EffortPoint{
			{}, {}, {}, {Seconds: 600, DistanceM: 2300}, {Seconds: 720, DistanceM: 2700},
		}}
		a3 := ActivityEfforts{ActivityID: 3, Points: []EffortPoint{
			{}, {}, {}, {}, {}, {Seconds: 900, DistanceM: 3300},
		}}

		pts, nActs := SelectFitPoints([]ActivityEfforts{dominant, a2, a3}, durs)
		if nActs != 3 {
			t.Fatalf("期望 3 次活动参与, 得到 %d", nActs)
		}
		if len(pts) > 3 {
			t.Fatalf("单次活动最多贡献 1 点 → 3 次活动最多 3 点, 得到 %d 点", len(pts))
		}
		// 活动 1 只能占一个时长
		fromAct1 := 0
		for _, p := range pts {
			// 活动 1 的距离恰好是 4.0 × 时长
			if p.DistanceM == p.Seconds*4.0 {
				fromAct1++
			}
		}
		if fromAct1 != 1 {
			t.Fatalf("活动 1 应只贡献 1 个点, 实际 %d", fromAct1)
		}
	})

	t.Run("活动数足够时沿用最快者", func(t *testing.T) {
		a1 := ActivityEfforts{ActivityID: 1, Points: []EffortPoint{{Seconds: 180, DistanceM: 1000}}}
		a2 := ActivityEfforts{ActivityID: 2, Points: []EffortPoint{{}, {Seconds: 300, DistanceM: 1200}}}
		pts, nActs := SelectFitPoints([]ActivityEfforts{a1, a2}, []int{180, 300})
		if nActs != 2 || len(pts) != 2 {
			t.Fatalf("期望 2 点/2 活动, 得到 %d 点/%d 活动", len(pts), nActs)
		}
		if pts[0].DistanceM != 1000 || pts[1].DistanceM != 1200 {
			t.Fatalf("点值不符: %+v", pts)
		}
	})
}

func TestFitCP3(t *testing.T) {
	// 用已知参数生成曲线：CS=3.5 m/s, D'=200 m, τ=120s
	const wantCS, wantD, wantTau = 3.5, 200.0, 120.0
	durs := []int{60, 120, 180, 300, 420, 600, 900, 1200}
	pts := make([]EffortPoint, 0, len(durs))
	for _, d := range durs {
		tf := float64(d)
		dist := wantD*(1-math.Exp(-tf/wantTau)) + wantCS*tf
		pts = append(pts, EffortPoint{Seconds: tf, DistanceM: dist})
	}

	r := FitCP3(pts)
	if r.Quality.RMSEM < 0 {
		t.Fatal("拟合失败")
	}
	if math.Abs(r.CS-wantCS)/wantCS > 0.01 {
		t.Fatalf("CS 期望 %v, 得到 %v", wantCS, r.CS)
	}
	if math.Abs(r.DPrime-wantD)/wantD > 0.05 {
		t.Fatalf("D' 期望 %v, 得到 %v", wantD, r.DPrime)
	}
	if math.Abs(r.Tau-wantTau)/wantTau > 0.10 {
		t.Fatalf("τ 期望 %v, 得到 %v", wantTau, r.Tau)
	}
	// Peak = CS + D'/τ = 3.5 + 200/120 ≈ 5.17（约 1.48× CS，可信）
	if math.Abs(r.Peak-(wantCS+wantD/wantTau)) > 0.15 {
		t.Fatalf("Peak 期望约 %v, 得到 %v", wantCS+wantD/wantTau, r.Peak)
	}
	if !r.PeakReliable {
		t.Fatalf("Peak 应标记为可信（%.2f ≤ 2.5×%.2f）", r.Peak, r.CS)
	}
	if r.Quality.R2 < 0.99 {
		t.Fatalf("R² 应接近 1, 得到 %v", r.Quality.R2)
	}

	t.Run("样本不足返回失败标记", func(t *testing.T) {
		bad := FitCP3([]EffortPoint{{Seconds: 180, DistanceM: 700}, {Seconds: 600, DistanceM: 2000}})
		if bad.Quality.RMSEM >= 0 {
			t.Fatal("少于 3 点时 RMSEM 应为 -1（失败标记）")
		}
	})

	t.Run("纯 2 参数数据仍能收敛", func(t *testing.T) {
		// τ 极小时退化为线性：D'=150, CS=3.2
		var linear []EffortPoint
		for _, d := range []int{180, 300, 600, 900} {
			linear = append(linear, EffortPoint{Seconds: float64(d), DistanceM: 150 + 3.2*float64(d)})
		}
		got := FitCP3(linear)
		if got.Quality.RMSEM < 0 || math.Abs(got.CS-3.2)/3.2 > 0.05 {
			t.Fatalf("应能拟合出 CS≈3.2, 得到 %+v", got)
		}
	})
}

func TestFitCP3TauNotIdentifiable(t *testing.T) {
	// 只有 ≥180s 的点：τ 无法辨识 → Peak 不可信
	pts := []EffortPoint{
		{Seconds: 180, DistanceM: 800},
		{Seconds: 300, DistanceM: 1200},
		{Seconds: 600, DistanceM: 2200},
		{Seconds: 900, DistanceM: 3300},
	}
	r := FitCP3(pts)
	if r.Quality.RMSEM < 0 {
		t.Fatal("应仍能拟合（只是 τ 不可辨识）")
	}
	if r.Tau < 30 {
		t.Fatalf("τ 不应低于下界 30, 得到 %v", r.Tau)
	}
	if r.PeakReliable {
		t.Fatalf("τ 贴在下界 %.1fs 时 Peak 不应标记可信（Peak=%.1f）", r.Tau, r.Peak)
	}
}
