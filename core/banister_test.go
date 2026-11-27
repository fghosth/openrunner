package core

import (
	"fmt"
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

func TestFitBanisterWeightedRecoversKnownCoefficients(t *testing.T) {
	// 无噪合成数据：任何 λ 都必须原样回收 (p0,k1,k2)。
	// 加权最小二乘只是重新加权零残差的解，不该改变精确解。
	loads := map[string]float64{}
	for d := 1; d <= 28; d++ {
		loads[fmt.Sprintf("2026-03-%02d", d)] = float64(40 + d)
	}
	const p0, k1, k2 = 5.0, 0.08, 0.05
	var races []RacePoint
	for _, day := range []string{"2026-03-10", "2026-03-17", "2026-03-24", "2026-03-28"} {
		di, _ := dayIndex(day)
		races = append(races, RacePoint{Date: day, Performance: p0 + k1*accumulated(loads, di, 45) - k2*accumulated(loads, di, 15)})
	}
	for _, lam := range []float64{0, 30, 90, 1e6} {
		m := FitBanisterWeighted(loads, races, 45, 15, lam)
		if m.RMSE < 0 {
			t.Fatalf("λ=%v 拟合失败", lam)
		}
		if math.Abs(m.P0-p0) > 1e-6 || math.Abs(m.K1-k1) > 1e-8 || math.Abs(m.K2-k2) > 1e-8 {
			t.Fatalf("λ=%v 未回收真值: p0=%v k1=%v k2=%v", lam, m.P0, m.K1, m.K2)
		}
	}
}

func TestFitBanisterLambdaZeroMatchesUnweighted(t *testing.T) {
	// λ=0 必须与旧的无权重实现逐值一致 —— 金标准 dump 走的就是这条路径，
	// 不一致说明这次改动动了既有口径。
	loads := map[string]float64{}
	for i := 0; i < 200; i++ {
		loads[daysAfter("2026-01-01", i)] = float64(20 + i%37)
	}
	races := []RacePoint{
		{Date: daysAfter("2026-01-01", 40), Performance: 3.10},
		{Date: daysAfter("2026-01-01", 90), Performance: 3.35},
		{Date: daysAfter("2026-01-01", 130), Performance: 3.28},
		{Date: daysAfter("2026-01-01", 180), Performance: 3.44},
	}
	base := FitBanisterWithTau(loads, races, 45, 15)
	zero := FitBanisterWeighted(loads, races, 45, 15, 0)
	// 容差而非精确相等：加权路径会多走一次 Sqrt(1) 与乘法，末位有 1e-15 级舍入差
	near := func(a, b float64) bool { return math.Abs(a-b) <= 1e-12*math.Max(1, math.Abs(b)) }
	if !near(base.P0, zero.P0) || !near(base.K1, zero.K1) || !near(base.K2, zero.K2) || !near(base.R2, zero.R2) || !near(base.RMSE, zero.RMSE) {
		t.Fatalf("λ=0 与无权重不一致: p0 %v/%v k1 %v/%v k2 %v/%v r2 %v/%v",
			base.P0, zero.P0, base.K1, zero.K1, base.K2, zero.K2, base.R2, zero.R2)
	}
}

func TestFitBanisterWeightedPrefersRecent(t *testing.T) {
	// 老成绩慢、新成绩快：λ 越小，最新日的拟合越贴住新成绩（且不过冲）。
	loads := map[string]float64{}
	for m := 1; m <= 12; m++ {
		loads[fmt.Sprintf("2026-%02d-15", m)] = 400
	}
	races := []RacePoint{
		{Date: "2026-01-15", Performance: 3.00},
		{Date: "2026-03-15", Performance: 3.02},
		{Date: "2026-11-15", Performance: 3.80},
		{Date: "2026-12-15", Performance: 3.82},
	}
	latest := "2026-12-15"
	prev := 0.0
	for _, lam := range []float64{1e9, 90, 30} {
		m := FitBanisterWeighted(loads, races, 45, 15, lam)
		got := m.Predict(latest)
		if got > 3.82+1e-9 {
			t.Fatalf("λ=%v 过冲最新实际: %v", lam, got)
		}
		if got < prev {
			t.Fatalf("λ=%v 应比上一档更贴新成绩: %v < %v", lam, got, prev)
		}
		prev = got
	}
	if prev <= 3.78 {
		t.Fatalf("λ=30 应明显贴住新成绩, 得到 %v", prev)
	}
}

func TestDetectFormBias(t *testing.T) {
	// 空负荷 + K=0 → Predict 恒等于 P0，便于精确控制偏差
	m := BanisterModel{P0: 3.5, RMSE: 0, loads: map[string]float64{}}
	const modelTime = 10000.0 / 3.5 // 2857.1s
	perfFor := func(secs float64) float64 { return 10000.0 / secs }

	t.Run("最近三条一致偏慢则触发", func(t *testing.T) {
		races := []RacePoint{
			{Date: "2026-11-01", Performance: perfFor(modelTime)},
			{Date: "2026-11-10", Performance: perfFor(modelTime * 0.96)},
			{Date: "2026-11-20", Performance: perfFor(modelTime * 0.96)},
			{Date: "2026-11-26", Performance: perfFor(modelTime * 0.96)},
		}
		b := DetectFormBias(m, races, 10000, 3, 0.02)
		if b.Direction != "modelSlow" || b.AnchorPerf <= 0 || b.N != 3 {
			t.Fatalf("应判成 modelSlow, 得到 %+v", b)
		}
	})

	t.Run("有快有慢是噪声不是偏差", func(t *testing.T) {
		races := []RacePoint{
			{Date: "2026-11-10", Performance: perfFor(modelTime * 0.94)},
			{Date: "2026-11-20", Performance: perfFor(modelTime * 1.06)},
			{Date: "2026-11-26", Performance: perfFor(modelTime * 0.96)},
		}
		if b := DetectFormBias(m, races, 10000, 3, 0.02); b.Direction != "none" {
			t.Fatalf("异号残差不该判成系统性偏差, 得到 %+v", b)
		}
	})

	t.Run("同号但幅度不足不触发", func(t *testing.T) {
		races := []RacePoint{
			{Date: "2026-11-10", Performance: perfFor(modelTime * 0.995)},
			{Date: "2026-11-20", Performance: perfFor(modelTime * 0.995)},
			{Date: "2026-11-26", Performance: perfFor(modelTime * 0.995)},
		}
		if b := DetectFormBias(m, races, 10000, 3, 0.02); b.Direction != "none" {
			t.Fatalf("0.5%% 偏差低于阈值, 不该触发, 得到 %+v", b)
		}
	})

	t.Run("样本不足两条直接返回 none", func(t *testing.T) {
		races := []RacePoint{{Date: "2026-11-26", Performance: 3.9}}
		if b := DetectFormBias(m, races, 10000, 3, 0.02); b.Direction != "none" {
			t.Fatalf("单条成绩不该判偏差, 得到 %+v", b)
		}
	})
}

// banisterSynth 用已知 5 参数生成无噪成绩点，供 τ 拟合的真值断言使用。
func banisterSynth(p0, k1, k2, tau1, tau2 float64) (map[string]float64, []RacePoint) {
	loads := map[string]float64{}
	for i := 0; i < 300; i++ {
		loads[daysAfter("2026-01-01", i)] = float64(30 + i%23)
	}
	var races []RacePoint
	for i := 2; i <= 10; i++ {
		d := daysAfter("2026-01-01", i*30)
		di, _ := dayIndex(d)
		races = append(races, RacePoint{
			Date:        d,
			Performance: p0 + k1*accumulated(loads, di, tau1) - k2*accumulated(loads, di, tau2),
		})
	}
	return loads, races
}

func TestFitBanisterFitTauRecoversKnownTau(t *testing.T) {
	// 无噪数据由已知 (p0,k1,k2,τ1,τ2) 生成 → 网格搜索必须把 τ 找回来。
	// 找不回 τ，界面上展示的「你的体能时间常数」就只是网格恰好落在哪的巧合。
	loads, races := banisterSynth(5.0, 0.08, 0.05, 60.0, 18.0)
	m := FitBanisterFitTau(loads, races, 0)
	if !m.TauFitted {
		t.Fatalf("9 条成绩应触发 τ 拟合")
	}
	if math.Abs(m.Tau1-60.0)/60.0 > 0.10 {
		t.Fatalf("τ1 回收偏差过大: %v", m.Tau1)
	}
	if math.Abs(m.Tau2-18.0)/18.0 > 0.10 {
		t.Fatalf("τ2 回收偏差过大: %v", m.Tau2)
	}
	if m.TauAtBoundary {
		t.Fatalf("真值在边界内，不该判成撞边界")
	}
	if m.Params != 5 || math.IsNaN(m.R2Adj) {
		t.Fatalf("拟合 τ 后参数个数应为 5、调整 R² 有定义: %v %v", m.Params, m.R2Adj)
	}
}

func TestFitBanisterFitTauFlagsUnidentifiable(t *testing.T) {
	// 真 τ1=400 远在边界外：SSE 在边界内单调下降，最优必然撞上界。
	// 此时返回的 τ 只是「边界内最优」，TauAtBoundary 必须为 True ——
	// 否则界面会把一个不可辨识的常数当成生理指标展示给用户。
	loads, races := banisterSynth(5.0, 0.08, 0.05, 400.0, 18.0)
	m := FitBanisterFitTau(loads, races, 0)
	if !m.TauAtBoundary {
		t.Fatalf("τ1 真值在边界外，必须报撞边界: τ1=%v", m.Tau1)
	}
	if math.Abs(m.Tau1-BanisterTau1Max) > 1e-9 {
		t.Fatalf("撞上界时 τ1 应等于上界, 得到 %v", m.Tau1)
	}
}

func TestFitBanisterFitTauNeverWorseThanFixed(t *testing.T) {
	// 搜索以固定 τ 的 RMSE 为下界起点，只接受严格更优的格点，
	// 所以拟合 τ 永远不会让拟合质量变差。
	loads, races := banisterSynth(5.0, 0.08, 0.05, 45.0, 15.0)
	fixed := FitBanisterWeighted(loads, races, 45, 15, 0)
	fitted := FitBanisterFitTau(loads, races, 0)
	if fitted.RMSE > fixed.RMSE+1e-12 {
		t.Fatalf("拟合 τ 反而变差: %v > %v", fitted.RMSE, fixed.RMSE)
	}
}

func TestFitBanisterFitTauFallsBackOnFewRaces(t *testing.T) {
	// 少于 BanisterMinRacesForTauFit 条时不许拟合 5 个参数：
	// 自由度 ≤ 0，调整 R² 无意义，界面上任何「拟合优度」都是自欺。
	loads, races := banisterSynth(5.0, 0.08, 0.05, 60.0, 18.0)
	few := races[:BanisterMinRacesForTauFit-1]
	m := FitBanisterFitTau(loads, few, 0)
	if m.TauFitted {
		t.Fatalf("%d 条成绩不该拟合 τ", len(few))
	}
	if m.Tau1 != 45 || m.Tau2 != 15 {
		t.Fatalf("应退回固定 τ=45/15, 得到 %v/%v", m.Tau1, m.Tau2)
	}
}
