package core

import (
	"math"
	"testing"
)

// rTSS 与配速区间恒等式（规格：Skiba 跑步区界，以 %CV（阈值速度）计：
// 6 区边界 80/87/95/105/119%；rTSS = 100 × secs × NP² / (CV² × 3600)）。

func TestRtss(t *testing.T) {
	const cv = 12.0 // 阈值速度 12 km/h = 5:00/km

	t.Run("阈值配速匀速 1 小时 → 100", func(t *testing.T) {
		if got := Rtss(3600, cv, cv); math.Abs(got-100) > 1e-9 {
			t.Fatalf("期望 100, 得到 %v", got)
		}
	})

	t.Run("时长线性", func(t *testing.T) {
		if got := Rtss(1800, cv, cv); math.Abs(got-50) > 1e-9 {
			t.Fatalf("期望 50, 得到 %v", got)
		}
	})

	t.Run("强度平方增长：1.1×CV → 121", func(t *testing.T) {
		if got := Rtss(3600, cv*1.1, cv); math.Abs(got-121) > 1e-9 {
			t.Fatalf("期望 121, 得到 %v", got)
		}
	})

	t.Run("无阈值或无速度 → 0", func(t *testing.T) {
		if Rtss(3600, 0, cv) != 0 || Rtss(3600, cv, 0) != 0 {
			t.Fatal("期望 0")
		}
	})
}

func TestNormalizedSpeedKph(t *testing.T) {
	t.Run("恒速 → 等于本身", func(t *testing.T) {
		if got := NormalizedSpeedKph([]float64{12, 12, 12, 12}); math.Abs(got-12) > 1e-9 {
			t.Fatalf("期望 12, 得到 %v", got)
		}
	})

	t.Run("波动 → 高于算术均值（4 次方权重）", func(t *testing.T) {
		speeds := []float64{10, 20, 10, 20}
		np := NormalizedSpeedKph(speeds)
		mean := 15.0
		if np <= mean {
			t.Fatalf("期望 NP > 算术均值, 得到 %v", np)
		}
		if np >= 20 {
			t.Fatalf("期望 NP ≤ 最大值, 得到 %v", np)
		}
	})

	t.Run("全零 → 0", func(t *testing.T) {
		if got := NormalizedSpeedKph([]float64{0, 0}); got != 0 {
			t.Fatalf("期望 0, 得到 %v", got)
		}
	})
}

func TestPaceTimeInZone(t *testing.T) {
	const cv = 12.0
	// 边界（%CV）：Z1 <80%, Z2 80-87%, Z3 87-95%, Z4 95-105%, Z5 105-119%, Z6 ≥119%
	t.Run("分区索引正确", func(t *testing.T) {
		cases := []struct {
			kph  float64
			want int
		}{
			{cv * 0.5, 0},  // Z1
			{cv * 0.85, 1}, // Z2
			{cv * 0.90, 2}, // Z3
			{cv * 1.00, 3}, // Z4
			{cv * 1.10, 4}, // Z5
			{cv * 1.30, 5}, // Z6
		}
		for _, c := range cases {
			if got := PaceZoneIndex(c.kph, cv); got != c.want {
				t.Fatalf("%v km/h 期望 Z%d, 得到 Z%d", c.kph, c.want+1, got+1)
			}
		}
	})

	t.Run("TiZ 计数（缺测跳过）", func(t *testing.T) {
		speeds := []float64{cv * 0.5, cv, 0, cv * 1.3}
		tiz := PaceTimeInZone(speeds, cv, 1)
		if tiz[0] != 1 || tiz[3] != 1 || tiz[5] != 1 {
			t.Fatalf("期望 Z1=1 Z4=1 Z6=1, 得到 %v", tiz)
		}
	})

	t.Run("无阈值 → 全零", func(t *testing.T) {
		tiz := PaceTimeInZone([]float64{10, 20}, 0, 1)
		if tiz != [6]float64{} {
			t.Fatalf("期望全零, 得到 %v", tiz)
		}
	})
}

func TestPowerLoadFormula(t *testing.T) {
	// PowerLoad 复用 Rtss：100 × 时长 × (NP/CP)² / 1h
	// 阈值功率 CP 跑满 1 小时 → 100 分
	if got := Rtss(3600, 280, 280); got != 100 {
		t.Fatalf("CP 跑 1 小时期望 100 分, 得到 %v", got)
	}
	// 半程 30 分钟 → 50 分
	if got := Rtss(1800, 280, 280); got != 50 {
		t.Fatalf("CP 跑 30 分钟期望 50 分, 得到 %v", got)
	}
	// NP = 1.5×CP → IF² = 2.25 → 1 小时 225 分
	if got := Rtss(3600, 420, 280); got < 224 || got > 226 {
		t.Fatalf("1.5 倍 CP 跑 1 小时期望约 225 分, 得到 %v", got)
	}
	// 参数非法 → 0
	if got := Rtss(3600, 0, 280); got != 0 {
		t.Fatalf("NP 为 0 时期望 0, 得到 %v", got)
	}
}

// NormalizedSpeed 必须和 NP 同一口径（30s 滑动平均）：单个 GPS 速度尖峰不该抬高它。
// 回归实测活动 281：原始 4 次方均值 14.82 vs 平滑 10.32（+43.6%），rTSS 差一倍。
func TestNormalizedSpeedSmoothsSpike(t *testing.T) {
	speeds := make([]float64, 600)
	for i := range speeds {
		speeds[i] = 12
	}
	speeds[320] = 120 // 10 倍速度尖峰（GPS 漂移量级）
	smoothed := NormalizedSpeed(speeds, 1)
	bare := NormalizedSpeedKph(speeds)
	if smoothed > 13 {
		t.Errorf("平滑后 NS 被尖峰抬高到 %.2f（基线 12）——30 秒平均没起作用", smoothed)
	}
	if !(bare > smoothed*1.1) {
		t.Errorf("裸 4 次方均值 %.2f 未明显高于平滑值 %.2f，测试失去区分度", bare, smoothed)
	}
	// 与 NP 同函数：同样输入同样输出（口径一致性）
	if math.Abs(NormalizedSpeed(speeds, 1)-NormalizedPower(speeds, 1)) > 1e-12 {
		t.Error("NormalizedSpeed 与 NormalizedPower 口径不一致")
	}
}

func TestPowerTimeInZone(t *testing.T) {
	const cp = 250.0
	t.Run("Coggan 七区边界", func(t *testing.T) {
		// 依次落 W1..W7：0.5/0.65/0.8/1.0/1.1/1.3/1.6 × CP
		powers := []float64{125, 162, 200, 250, 275, 325, 400}
		tiz := PowerTimeInZone(powers, cp, 10)
		want := [7]float64{10, 10, 10, 10, 10, 10, 10}
		if tiz != want {
			t.Fatalf("期望 %v, 得到 %v", want, tiz)
		}
	})
	t.Run("阈值（100% CP）必须落在 W4 不是 W1", func(t *testing.T) {
		// 旧实现复用跑步 %CV 界（0.80/0.87/0.95/1.05/1.19），100% CP 会落 W4，
		// 但 70% CP 的耐力骑会整段落 W1——这里钉住 70% 应落 W2（Coggan 有氧区）。
		tiz := PowerTimeInZone([]float64{0.7 * cp}, cp, 1)
		if tiz[1] != 1 {
			t.Fatalf("70%% CP 应落 W2，得到 %v", tiz)
		}
	})
	t.Run("零值与无 CP", func(t *testing.T) {
		if got := PowerTimeInZone([]float64{0, 0, 250}, cp, 1); got[0]+got[1]+got[2] != 0 || got[3] != 1 {
			t.Fatalf("0 功率不应计时间，得到 %v", got)
		}
		if got := PowerTimeInZone([]float64{250}, 0, 1); got != ([7]float64{}) {
			t.Fatalf("无 CP 应返回全 0，得到 %v", got)
		}
	})
}
