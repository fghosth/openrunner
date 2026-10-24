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
