package core

import (
	"math"
	"reflect"
	"testing"
	"time"
)

// PMC 恒等式（规格：docs/goldencheetah-integration/03 §A2/A3；金标准：GC PMCData.calculateMetrics）。

func testPMCInput() PMCInput {
	return PMCInput{
		From:    "2026-01-01",
		To:      "2026-12-31",
		LtsDays: 42,
		StsDays: 7,
		Actual:  map[string]float64{},
		Planned: map[string]float64{},
		SBToday: false,
	}
}

func TestPMCConstantLoadConverges(t *testing.T) {
	in := testPMCInput()
	// 1/1 起每天恒定 100 负荷，365 天（IIR 需 ~4×τ 收敛）
	for i := 0; i < 365; i++ {
		in.Actual[addDays("2026-01-01", i)] = 100
	}
	out := ComputePMC(in)

	// 稳态：IIR 滤波 y = x(1−a)+y·a 的稳态为 x → LTS/STS→100, SB→0
	n := len(out.Dates)
	last := n - 1
	if math.Abs(out.Lts[last]-100) > 0.5 {
		t.Fatalf("期望 LTS 收敛到 ≈100, 得到 %v", out.Lts[last])
	}
	if math.Abs(out.Sts[last]-100) > 0.5 {
		t.Fatalf("期望 STS 收敛到 ≈100, 得到 %v", out.Sts[last])
	}
	if math.Abs(out.Sb[last]) > 0.5 {
		t.Fatalf("期望 SB→0, 得到 %v", out.Sb[last])
	}
}

func TestPMCSeeding(t *testing.T) {
	in := testPMCInput()
	in.Seeds = []SeasonSeed{{Date: "2026-01-01", Value: 300}}

	out := ComputePMC(in)
	// 播种日 LTS/STS 精确等于 seed（不从 0 爬升）
	if out.Lts[0] != 300 || out.Sts[0] != 300 {
		t.Fatalf("期望播种 300, 得到 LTS=%v STS=%v", out.Lts[0], out.Sts[0])
	}
	// 之后无负荷，按指数衰减：d1 = 300·exp(−1/42)
	want := 300 * math.Exp(-1.0/42)
	if math.Abs(out.Lts[1]-want) > 1e-9 {
		t.Fatalf("期望衰减 %v, 得到 %v", want, out.Lts[1])
	}
}

func TestPMCSBDayOffset(t *testing.T) {
	in := testPMCInput()
	in.Actual["2026-01-01"] = 100 // 单天负荷，之后休息

	outDefault := ComputePMC(in) // SBToday=false → SB 画在次日
	if outDefault.Sb[1] == 0 {
		t.Fatal("期望 SB 出现在 day+1")
	}
	in.SBToday = true
	outToday := ComputePMC(in)
	if outToday.Sb[0] == 0 {
		t.Fatal("期望 SB 出现在当天")
	}
}

func TestPMCTodaySpecialCase(t *testing.T) {
	today := "2026-02-01"
	in := testPMCInput()
	in.Today = today
	// 计划 100，实际也有 50 → 今日取实际
	in.Planned[today] = 100
	in.Actual[today] = 50

	out := ComputePMC(in)
	n := len(out.Dates)
	last := n - 1
	if out.ExpectedLts[last] == out.PlannedLts[last] {
		t.Fatal("今日 expected 应取实际（50 < 100, LTS 更小）")
	}
	// 未来日期只取计划
	in.Planned[addDays(today, 10)] = 80
	out2 := ComputePMC(in)
	idx := indexOf(out2.Dates, addDays(today, 10))
	if out2.ExpectedLts[idx] == 0 {
		t.Fatal("未来日期 expected 应来自计划")
	}
}

func TestPMCRampRate(t *testing.T) {
	in := testPMCInput()
	for i := 0; i < 14; i++ {
		in.Actual[addDays("2026-01-01", i)] = 100
	}
	out := ComputePMC(in)
	// RR = STS 窗口内 LTS 日增量之和；STeady 100 负荷下 LTS 日增量趋于 0
	last := len(out.Dates) - 1
	if math.Abs(out.Rr[last]) > 5 {
		t.Fatalf("稳态下 RR 应接近 0, 得到 %v", out.Rr[last])
	}
	// 陡增段 RR 为正
	if out.Rr[7] <= 0 {
		t.Fatalf("爬升期 RR 应为正, 得到 %v", out.Rr[7])
	}
}

func TestPMCExpectedPastUsesActual(t *testing.T) {
	in := testPMCInput()
	in.Today = "2026-02-15"
	in.Actual["2026-01-10"] = 200
	in.Planned["2026-01-10"] = 100 // 过去：计划被忽略

	out := ComputePMC(in)
	idx := indexOf(out.Dates, "2026-01-10")
	if math.Abs(out.Lts[idx]-out.ExpectedLts[idx]) > 1e-9 {
		t.Fatal("过去日期 expected 应等于 actual")
	}
}

func TestPMCDateAlignment(t *testing.T) {
	in := testPMCInput()
	in.To = "2026-03-01" // 覆盖 helper 的默认值，专测日期对齐
	out := ComputePMC(in)
	// 从 From 到 To 逐日对齐
	if len(out.Dates) != daysBetween("2026-01-01", "2026-03-01")+1 {
		t.Fatalf("期望 %d 天, 得到 %d", daysBetween("2026-01-01", "2026-03-01")+1, len(out.Dates))
	}
	if out.Dates[0] != "2026-01-01" || !reflect.DeepEqual(out.Dates[len(out.Dates)-1], "2026-03-01") {
		t.Fatalf("日期端点错误: %s..%s", out.Dates[0], out.Dates[len(out.Dates)-1])
	}
}

func addDays(date string, n int) string {
	t, err := time.ParseInLocation("2006-01-02", date, time.UTC)
	if err != nil {
		return date
	}
	return t.AddDate(0, 0, n).Format("2006-01-02")
}

func daysBetween(from, to string) int {
	f, _ := time.ParseInLocation("2006-01-02", from, time.UTC)
	t, _ := time.ParseInLocation("2006-01-02", to, time.UTC)
	return int(t.Sub(f).Hours() / 24)
}
