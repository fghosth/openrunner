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
