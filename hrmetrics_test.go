package core

import (
	"math"
	"testing"
)

// TRIMP 家族恒等式（规格来源 docs/goldencheetah-integration/02 §4、06 §3.2）。
// 公式：TRIMP = 分钟 × %HRR × 0.64 × exp(Ksex × %HRR)，Ksex 男 1.92 / 女 1.67。

func TestTrimpPoints(t *testing.T) {
	// 阈值心率骑 1 小时的 TRIMP(100) 分母构造
	const maxHr, restHr, ltHr = 190.0, 50.0, 160.0

	t.Run("平均心率=RestHR → 0", func(t *testing.T) {
		got := TrimpPoints(3600, restHr, maxHr, restHr, 1.92)
		if got != 0 {
			t.Fatalf("期望 0, 得到 %v", got)
		}
	})

	t.Run("低于 RestHR → 0（不出现负值）", func(t *testing.T) {
		got := TrimpPoints(3600, restHr-10, maxHr, restHr, 1.92)
		if got != 0 {
			t.Fatalf("期望 0, 得到 %v", got)
		}
	})

	t.Run("时长线性：两倍时长两倍分", func(t *testing.T) {
		a := TrimpPoints(1800, 150, maxHr, restHr, 1.92)
		b := TrimpPoints(3600, 150, maxHr, restHr, 1.92)
		if math.Abs(b-2*a) > 1e-9 {
			t.Fatalf("期望线性, 得到 %v vs %v", a, b)
		}
	})

	t.Run("Ksex 影响：同参数女性分值更小", func(t *testing.T) {
		m := TrimpPoints(3600, 170, maxHr, restHr, 1.92)
		f := TrimpPoints(3600, 170, maxHr, restHr, 1.67)
		if f >= m {
			t.Fatalf("期望女性 Ksex 分值更低, 得到 %v vs %v", f, m)
		}
	})
}

func TestTrimp100(t *testing.T) {
	const maxHr, restHr, ltHr = 190.0, 50.0, 160.0
	const ksex = 1.92

	t.Run("阈值心率 1 小时 → 100", func(t *testing.T) {
		trimp := TrimpPoints(3600, ltHr, maxHr, restHr, ksex)
		got := Trimp100(trimp, maxHr, restHr, ltHr, ksex)
		if math.Abs(got-100) > 1e-9 {
			t.Fatalf("期望 100, 得到 %v", got)
		}
	})

	t.Run("TRIMP=0 → 0（不除零）", func(t *testing.T) {
		if got := Trimp100(0, maxHr, restHr, ltHr, ksex); got != 0 {
			t.Fatalf("期望 0, 得到 %v", got)
		}
	})
}

func TestTrimpZonal(t *testing.T) {
	// GC TRIMP Zonal（Edwards 系）：按 %MaxHR 分 5 区，逐秒系数累加
	// 区界 64/77/84/90/95% MHR；系数 0.84/1.65/2.57/4.01/5.91
	const maxHr = 200.0

	t.Run("恒定心率 95-100% 区 60s", func(t *testing.T) {
		hrs := make([]float64, 60)
		for i := range hrs {
			hrs[i] = 195 // 97.5% MHR → zone5
		}
		got := TrimpZonal(hrs, maxHr, 1)
		if math.Abs(got-5.91) > 1e-9 { // 60s × 5.91/60
			t.Fatalf("期望 5.91, 得到 %v", got)
		}
	})

	t.Run("缺测样本跳过（0 心率不计时间）", func(t *testing.T) {
		hrs := []float64{195, 0, 195}
		got := TrimpZonal(hrs, maxHr, 1)
		if math.Abs(got-5.91*2/60) > 1e-9 {
			t.Fatalf("期望 %v, 得到 %v", 5.91*2/60, got)
		}
	})

	t.Run("心率 TiZ 分布", func(t *testing.T) {
		hrs := []float64{130, 130, 160, 0, 195} // 65%→z1×2, 80%→z2, 缺测, 97.5%→z5
		tiz := HrTimeInZone(hrs, maxHr, 1)
		if tiz[0] != 2 || tiz[1] != 1 || tiz[4] != 1 {
			t.Fatalf("期望 [2,1,0,0,1], 得到 %v", tiz)
		}
	})
}

func TestPeakHeartRate(t *testing.T) {
	t.Run("取最大值并剔除缺测", func(t *testing.T) {
		p := PeakHeartRate([]float64{120, 0, 176, 155, 0})
		if p.MaxHr != 176 || p.Samples != 3 {
			t.Fatalf("期望 {176 3}, 得到 %+v", p)
		}
	})

	t.Run("剔除传感器噪声尖峰", func(t *testing.T) {
		p := PeakHeartRate([]float64{140, 245, 172}) // 245 超过合理上界
		if p.MaxHr != 172 || p.Samples != 2 {
			t.Fatalf("期望 {172 2}, 得到 %+v", p)
		}
	})

	t.Run("空通道", func(t *testing.T) {
		p := PeakHeartRate(nil)
		if p.MaxHr != 0 || p.Samples != 0 {
			t.Fatalf("期望零值, 得到 %+v", p)
		}
	})
}

func TestObserveHeartRate(t *testing.T) {
	t.Run("持续峰值抵御孤立尖峰", func(t *testing.T) {
		// 60 秒稳定 170，中间两个孤立尖峰 205（光电伪影）
		hrs := make([]float64, 60)
		for i := range hrs {
			hrs[i] = 170
		}
		hrs[10], hrs[30] = 205, 205

		o := ObserveHeartRate(hrs, 1, 10)
		if o.Peak != 205 {
			t.Fatalf("单点峰值期望 205, 得到 %v", o.Peak)
		}
		// 10 秒窗口里含 1 个 205 + 9 个 170 → 均值 173.5
		if o.Sustained < 173 || o.Sustained > 174 {
			t.Fatalf("持续峰值期望约 173.5, 得到 %v", o.Sustained)
		}
		if o.Quality != 1 {
			t.Fatalf("期望质量 1.0, 得到 %v", o.Quality)
		}
	})

	t.Run("低质量数据被识别", func(t *testing.T) {
		// 70% 缺测：模拟光电腕表在力量训练中的表现
		hrs := []float64{0, 0, 0, 0, 0, 0, 0, 150, 0, 0}
		o := ObserveHeartRate(hrs, 4, 12)
		if o.Quality > 0.2 {
			t.Fatalf("期望质量约 0.1, 得到 %v", o.Quality)
		}
	})

	t.Run("空通道", func(t *testing.T) {
		o := ObserveHeartRate(nil, 1, 30)
		if o.Peak != 0 || o.Sustained != 0 || o.Quality != 0 {
			t.Fatalf("期望零值, 得到 %+v", o)
		}
	})
}
