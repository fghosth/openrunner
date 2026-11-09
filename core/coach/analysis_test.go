package coach

import (
	"math"
	"testing"
)

// 票 06：单课分析（Pa:Hr 解耦 / EF / 心率漂移）。
// 期望值全部手工推算；数值口径与 analyze_session.py 的 series_halves 对齐。

// mkStream 生成 n 点、间隔 interval 秒的 hr/speed 双流。
func mkStream(n, interval int, hr func(i int) float64, spd func(i int) float64) ([]float64, []float64) {
	h := make([]float64, n)
	s := make([]float64, n)
	for i := 0; i < n; i++ {
		h[i] = hr(i)
		s[i] = spd(i)
	}
	return h, s
}

func TestAnalyzeSessionSteady(t *testing.T) {
	// 70 分钟 @1s 间隔太长，用 10s 间隔：420 点 = 70 分钟。
	// 全程匀速 12 km/h、心率 150 → 解耦 0、漂移 0、EF = 200 m/min / 150 = 1.3333
	hr, spd := mkStream(420, 10, func(int) float64 { return 150 }, func(int) float64 { return 12 })
	a := AnalyzeActivity(4200, 10, hr, spd)
	if !a.Applicable {
		t.Fatalf("70 分钟双流应适用：%s", a.Reason)
	}
	if math.Abs(a.DecouplingPct) > 0.01 {
		t.Errorf("匀速恒心率解耦应 ≈0，得 %.3f", a.DecouplingPct)
	}
	if a.DecouplingVerdict != "ok" {
		t.Errorf("解耦判定应 ok，得 %s", a.DecouplingVerdict)
	}
	if math.Abs(a.EFPerBpm-200.0/150.0) > 1e-9 {
		t.Errorf("EF 应 1.3333，得 %.4f", a.EFPerBpm)
	}
	if math.Abs(a.HrDriftPct) > 0.01 || a.HrDriftVerdict != "ok" {
		t.Errorf("恒心率漂移应 ≈0/ok，得 %.2f/%s", a.HrDriftPct, a.HrDriftVerdict)
	}
}

func TestAnalyzeSessionDecoupling(t *testing.T) {
	// 注意中点偏移：跳过前 600s 后有效区间 [600,4190]，中点 2395 → 前半含 30 个
	// 「后半形态」点（i=210..239）。手算第一版按 i=210 对半分得 9.52 是错的，
	// 正确值 ra=(150·12+30·11.4)/180 ÷ (150·150+30·157.5)/180 = 0.0786777，
	// rb = 11.4/157.5 = 0.0723810 → dec = 8.0032%（series_halves 同款中点语义）
	hr, spd := mkStream(420, 10,
		func(i int) float64 {
			if i < 210 {
				return 150
			}
			return 157.5
		},
		func(i int) float64 {
			if i < 210 {
				return 12
			}
			return 11.4
		})
	a := AnalyzeActivity(4200, 10, hr, spd)
	if math.Abs(a.DecouplingPct-8.0032) > 0.01 {
		t.Errorf("解耦应 ≈8.00%%，得 %.4f", a.DecouplingPct)
	}
	if a.DecouplingVerdict != "moderate" {
		t.Errorf("5–10%% 应 moderate，得 %s", a.DecouplingVerdict)
	}
}

func TestAnalyzeSessionNotApplicable(t *testing.T) {
	hr, spd := mkStream(300, 10, func(int) float64 { return 150 }, func(int) float64 { return 12 })
	// 50 分钟 < 60 → 不适用（analyze_session.py MIN_STEADY_MIN）
	a := AnalyzeActivity(3000, 10, hr, spd)
	if a.Applicable || a.Reason == "" {
		t.Errorf("短课应不适用且给原因，得 %+v", a)
	}
	// 无心率流
	a2 := AnalyzeActivity(4200, 10, nil, spd)
	if a2.Applicable || a2.Reason == "" {
		t.Errorf("无双流应不适用，得 %+v", a2)
	}
}

func TestAnalyzeSessionWarmupSkipped(t *testing.T) {
	// 前 10 分钟是垃圾前导（心率 0 / 速度 0，NP 轮教训的边界形态），
	// 之后 65 分钟匀速恒心率 → 结果必须与理想稳态一致
	hr, spd := mkStream(450, 10,
		func(i int) float64 {
			if i < 60 {
				return 0
			}
			return 150
		},
		func(i int) float64 {
			if i < 60 {
				return 0
			}
			return 12
		})
	a := AnalyzeActivity(4500, 10, hr, spd)
	if !a.Applicable {
		t.Fatalf("应适用：%s", a.Reason)
	}
	if math.Abs(a.DecouplingPct) > 0.01 {
		t.Errorf("热身垃圾不得影响解耦，得 %.3f", a.DecouplingPct)
	}
}

func TestAnalyzeSessionHrDrift(t *testing.T) {
	// 匀速不变、心率线性上升：前 15 分钟（跳过热身后）均值 vs 末 15 分钟均值
	// 心率从 140 线性升到 160（420 点）：early≈140+ (600..1500s)/4200*20 → 区间 [600,1500)
	// 均值 = 140 + 20*(1050/4200) = 145；late 区间 [3300,4200)：均值 = 140+20*(3750/4200)=157.857
	// drift = (157.857-145)/145*100 = 8.87% → high
	hr, spd := mkStream(420, 10,
		func(i int) float64 { return 140 + 20*float64(i)/420 },
		func(int) float64 { return 12 })
	a := AnalyzeActivity(4200, 10, hr, spd)
	if math.Abs(a.HrDriftPct-8.867) > 0.05 {
		t.Errorf("漂移应 ≈8.87%%，得 %.3f", a.HrDriftPct)
	}
	if a.HrDriftVerdict != "high" {
		t.Errorf(">5%% 应 high，得 %s", a.HrDriftVerdict)
	}
	// 匀速 + 心率升：Pa:Hr 解耦恰应捕捉到（前半均值 147.1 / 后半 155.7 → ≈5.5%），
	// 与漂移是两个窗口口径，但方向一致
	if a.DecouplingPct < 5 || a.DecouplingPct > 8 || a.DecouplingVerdict == "ok" {
		t.Errorf("匀速心率爬升时解耦应为 moderate 正值，得 %.3f/%s", a.DecouplingPct, a.DecouplingVerdict)
	}
}
