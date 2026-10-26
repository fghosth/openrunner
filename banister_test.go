package core

import (
	"math"
	"testing"
)

func TestRaceRiegelEquivalent(t *testing.T) {
	// 5K 20:00 → 10K 等价时间应约 41:40（Riegel 1.06）
	eq := RaceRiegelEquivalent(5000, 1200, 10000)
	want := 1200 * math.Pow(2, 1.06)
	if math.Abs(eq-want) > 0.5 {
		t.Fatalf("期望 %v, 得到 %v", want, eq)
	}
	// 同距离 → 原值
	if got := RaceRiegelEquivalent(10000, 3000, 10000); math.Abs(got-3000) > 1e-9 {
		t.Fatalf("同距离应等于原用时, 得到 %v", got)
	}
}

func TestFitBanister(t *testing.T) {
	// 构造 100 天训练负荷：前 60 天每天 50，之后每天 30
	loads := map[string]float64{}
	start := "2026-01-01"
	for i := 0; i < 100; i++ {
		d := daysAfter(start, i)
		if i < 60 {
			loads[d] = 50
		} else {
			loads[d] = 30
		}
	}

	// 用已知参数生成表现
	const wantP0, wantK1, wantK2 = 3.0, 0.02, 0.01
	tau1, tau2 := 45.0, 15.0
	raceDays := []int{40, 70, 90}
	races := make([]RacePoint, 0, len(raceDays))
	for _, di := range raceDays {
		d := daysAfter(start, di)
		idx, _ := dayIndex(d)
		a := accumulated(loads, idx, tau1)
		b := accumulated(loads, idx, tau2)
		races = append(races, RacePoint{
			Date:        d,
			Performance: wantP0 + wantK1*a - wantK2*b,
		})
	}

	m := FitBanisterWithTau(loads, races, tau1, tau2)
	if m.RMSE < 0 {
		t.Fatal("拟合失败")
	}
	if math.Abs(m.P0-wantP0) > 0.05 {
		t.Fatalf("P0 期望 %v, 得到 %v", wantP0, m.P0)
	}
	if math.Abs(m.K1-wantK1)/wantK1 > 0.05 {
		t.Fatalf("K1 期望 %v, 得到 %v", wantK1, m.K1)
	}
	if math.Abs(m.K2-wantK2)/wantK2 > 0.05 {
		t.Fatalf("K2 期望 %v, 得到 %v", wantK2, m.K2)
	}
	if m.R2 < 0.999 {
		t.Fatalf("已知参数生成的数据应完美拟合, R²=%v", m.R2)
	}
	// 3 个成绩点拟合 3 个参数 → 自由度 0 → 必须标记过拟合
	if !m.Overfit {
		t.Fatal("n=3 时应标记 Overfit（自由度 0，R² 恒为 1）")
	}

	t.Run("预测可调用", func(t *testing.T) {
		p := m.Predict(daysAfter(start, 95))
		if p <= 0 {
			t.Fatalf("预测值应为正, 得到 %v", p)
		}
	})

	t.Run("成绩点不足", func(t *testing.T) {
		bad := FitBanister(loads, races[:2])
		if bad.RMSE >= 0 {
			t.Fatal("少于 3 个成绩点应返回 RMSE=-1")
		}
	})
}

// daysAfter 返回 base 之后 n 天的 YYYY-MM-DD。
func daysAfter(base string, n int) string {
	idx, _ := dayIndex(base)
	d := int(idx) + n
	return timeFromDays(d)
}

func timeFromDays(days int) string {
	return timeFromIndex(float64(days)).Format("2006-01-02")
}
