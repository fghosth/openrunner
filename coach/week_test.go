package coach

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// 周校验规则引擎的测试。语义逐条对齐
// running-coach/scripts/check_week.py（MIT），金标准对照见
// openrunner/scripts/coach-check.py。

func TestCheckWeek_VolumeProgression(t *testing.T) {
	// 构造 4 周历史（每周 3 课 × 100min = 300min，base≥240 避开小步幅豁免），计划周不同总量
	hist := []TrainingUnit{}
	for w := 0; w < 4; w++ {
		for d := 0; d < 3; d++ {
			hist = append(hist, TrainingUnit{
				Date:    weekDate(w, d),
				Minutes: 100, KM: 16,
			})
		}
	}
	cases := []struct {
		name  string
		total float64
		want  string
	}{
		{"持平", 300, "OK"},
		{"+16% WARN", 348, "WARN"}, // 300*1.16=348
		{"+21% FAIL", 363, "FAIL"}, // 300*1.21=363
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			per := c.total / 3
			plan := []TrainingUnit{
				{Date: weekDate(4, 0), Type: "easy", Minutes: per, KM: per / 6},
				{Date: weekDate(4, 2), Type: "easy", Minutes: per, KM: per / 6},
				{Date: weekDate(4, 4), Type: "long", Minutes: per, KM: per / 6},
			}
			vols := run(t, WeekCheckInput{WeekRole: "build", History: hist, Plan: plan}, "volume")
			if vols[0].Level != c.want {
				t.Fatalf("volume 判定 = %s (%s)，期望 %s", vols[0].Level, vols[0].Detail, c.want)
			}
		})
	}
}

func TestCheckWeek_VolumeSmallStepExemption(t *testing.T) {
	// 低量（<240min）且绝对增幅 ≤30min：即使百分比超限也放行（S3 实践口径）
	hist := []TrainingUnit{}
	for d := 0; d < 3; d++ {
		hist = append(hist, TrainingUnit{Date: weekDate(3, d), Minutes: 40, KM: 6})
	}
	plan := []TrainingUnit{
		{Date: weekDate(4, 0), Type: "easy", Minutes: 50, KM: 8},
		{Date: weekDate(4, 2), Type: "easy", Minutes: 50, KM: 8},
		{Date: weekDate(4, 4), Type: "long", Minutes: 50, KM: 8},
	}
	vols := run(t, WeekCheckInput{WeekRole: "build", History: hist, Plan: plan}, "volume")
	if vols[0].Level == "FAIL" {
		t.Errorf("低量小步幅被误判 FAIL: %s", vols[0].Detail)
	}
}

func TestCheckWeek_VolumeAfterLightWeek(t *testing.T) {
	// 上周明显偏轻（<85% 近三周峰值）→ 与正常周比而不是与轻周比
	hist := []TrainingUnit{}
	for w := 0; w < 3; w++ { // 前三周各 200min
		for d := 0; d < 2; d++ {
			hist = append(hist, TrainingUnit{Date: weekDate(w, d), Minutes: 100, KM: 16})
		}
	}
	for d := 0; d < 2; d++ { // 上周只跑 100min（轻）
		hist = append(hist, TrainingUnit{Date: weekDate(3, d), Minutes: 50, KM: 8})
	}
	plan := []TrainingUnit{ // 本周 210min：对轻周 +110%，对正常周 +5%
		{Date: weekDate(4, 0), Type: "easy", Minutes: 105, KM: 17},
		{Date: weekDate(4, 2), Type: "long", Minutes: 105, KM: 17},
	}
	vols := run(t, WeekCheckInput{WeekRole: "build", History: hist, Plan: plan}, "volume")
	if vols[0].Level != "OK" {
		t.Errorf("轻周后回归被误判: %s %s", vols[0].Level, vols[0].Detail)
	}
}

func TestCheckWeek_DeloadRole(t *testing.T) {
	hist := []TrainingUnit{}
	for w := 0; w < 3; w++ {
		for d := 0; d < 2; d++ {
			hist = append(hist, TrainingUnit{Date: weekDate(w, d), Minutes: 90, KM: 14})
		}
	}
	// deload 周但总量没降 → WARN
	plan := []TrainingUnit{
		{Date: weekDate(3, 0), Type: "easy", Minutes: 90, KM: 14},
		{Date: weekDate(3, 2), Type: "easy", Minutes: 90, KM: 14},
	}
	ds := run(t, WeekCheckInput{WeekRole: "deload", History: hist, Plan: plan}, "volume")
	if ds[0].Level != "WARN" {
		t.Errorf("deload 周未减量应 WARN，得 %s: %s", ds[0].Level, ds[0].Detail)
	}
	// 真减量 → OK
	plan2 := []TrainingUnit{
		{Date: weekDate(3, 0), Type: "easy", Minutes: 55, KM: 9},
		{Date: weekDate(3, 2), Type: "easy", Minutes: 55, KM: 9},
	}
	ds2 := run(t, WeekCheckInput{WeekRole: "deload", History: hist, Plan: plan2}, "volume")
	if ds2[0].Level != "OK" {
		t.Errorf("deload 真减量应 OK，得 %s: %s", ds2[0].Level, ds2[0].Detail)
	}
}

func TestCheckWeek_Spike(t *testing.T) {
	// 近 30 天最长 10km；计划 15km → FAIL（S1）
	hist := []TrainingUnit{
		{Date: weekDate(3, 0), Minutes: 50, KM: 10},
		{Date: weekDate(3, 2), Minutes: 30, KM: 6},
	}
	plan := []TrainingUnit{
		{Date: weekDate(4, 0), Type: "easy", Minutes: 40, KM: 8},
		{Date: weekDate(4, 4), Type: "long", Minutes: 75, KM: 15},
	}
	sp := run(t, WeekCheckInput{WeekRole: "build", History: hist, Plan: plan}, "spike")
	found := false
	for _, v := range sp {
		if v.Level == "FAIL" {
			found = true
		}
	}
	if !found {
		t.Errorf("15km 比 30 天最长 10km 多 50%%，应 FAIL；得 %+v", sp)
	}
	// 合规长课 11km（+10% 内）→ OK
	plan2 := []TrainingUnit{
		{Date: weekDate(4, 0), Type: "easy", Minutes: 40, KM: 8},
		{Date: weekDate(4, 4), Type: "long", Minutes: 55, KM: 10.8},
	}
	sp2 := run(t, WeekCheckInput{WeekRole: "build", History: hist, Plan: plan2}, "spike")
	for _, v := range sp2 {
		if v.Level == "FAIL" {
			t.Errorf("10.8km 在 +10%% 内不应 FAIL: %s", v.Detail)
		}
	}
}

func TestCheckWeek_SpikeRaceExempt(t *testing.T) {
	// 比赛豁免：race 类型超长只报 OK（附超幅），不 FAIL
	hist := []TrainingUnit{{Date: weekDate(3, 0), Minutes: 50, KM: 10}}
	plan := []TrainingUnit{{Date: weekDate(4, 6), Type: "race", Minutes: 90, KM: 21}}
	sp := run(t, WeekCheckInput{WeekRole: "race", History: hist, Plan: plan}, "spike")
	for _, v := range sp {
		if v.Level == "FAIL" {
			t.Errorf("比赛应豁免单次 +10%% 规则，得 FAIL: %s", v.Detail)
		}
	}
}

func TestCheckWeek_DeloadRhythm(t *testing.T) {
	// 连续 3 周递增且无轻周（min ≥ 0.85×峰值），第 4 周继续增 → WARN
	hist := []TrainingUnit{}
	for w, mins := range []float64{120, 125, 130} {
		hist = append(hist, TrainingUnit{Date: weekDate(w, 0), Minutes: mins, KM: mins / 6})
	}
	plan := []TrainingUnit{{Date: weekDate(3, 0), Type: "easy", Minutes: 135, KM: 22}}
	dr := run(t, WeekCheckInput{WeekRole: "build", History: hist, Plan: plan}, "deload-rhythm")
	if len(dr) == 0 || dr[0].Level != "WARN" {
		t.Errorf("连续递增无减量应 WARN，得 %+v", dr)
	}
	// 中间插一个轻周（100 < 0.85×130）→ 不 WARN
	// 中间周偏轻（100 < 0.85×130）→ 不 WARN
	hist2 := []TrainingUnit{
		{Date: weekDate(0, 0), Minutes: 120, KM: 20},
		{Date: weekDate(1, 0), Minutes: 100, KM: 16},
		{Date: weekDate(2, 0), Minutes: 130, KM: 21},
	}
	all, err := CheckWeek(WeekCheckInput{WeekRole: "build", History: hist2, Plan: plan})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range all {
		if v.Rule == "deload-rhythm" && v.Level == "WARN" {
			t.Errorf("有轻周时不应 WARN: %s", v.Detail)
		}
	}
}

func TestCheckWeek_LongShareAndHardDays(t *testing.T) {
	hist := []TrainingUnit{{Date: weekDate(3, 0), Minutes: 60, KM: 10}}
	// 长课占比过高：两课 30+70 → 70%
	plan := []TrainingUnit{
		{Date: weekDate(4, 0), Type: "easy", Minutes: 30, KM: 5},
		{Date: weekDate(4, 4), Type: "long", Minutes: 70, KM: 12},
	}
	ls := run(t, WeekCheckInput{WeekRole: "build", History: hist, Plan: plan}, "long-share")
	if ls[0].Level != "WARN" {
		t.Errorf("长课 70%% 占比应 WARN，得 %s", ls[0].Detail)
	}
	// 硬课背靠背 → FAIL
	plan2 := []TrainingUnit{
		{Date: weekDate(4, 1), Type: "threshold", Minutes: 50, KM: 9},
		{Date: weekDate(4, 2), Type: "intervals", Minutes: 50, KM: 9},
		{Date: weekDate(4, 4), Type: "easy", Minutes: 40, KM: 7},
	}
	hd := run(t, WeekCheckInput{WeekRole: "build", History: hist, Plan: plan2}, "hard-days")
	fail := false
	for _, v := range hd {
		if v.Level == "FAIL" {
			fail = true
		}
	}
	if !fail {
		t.Errorf("硬课背靠背应 FAIL，得 %+v", hd)
	}
}

func TestCheckWeek_IntensityAndRest(t *testing.T) {
	hist := []TrainingUnit{{Date: weekDate(3, 0), Minutes: 60, KM: 10}}
	// 5 个质量课 ×60min：quality=0.4*180+0.3*120=108 → 108/420=25.7% >25% WARN；
	// 且 7 天全有课 → rest WARN
	plan := []TrainingUnit{
		{Date: weekDate(4, 0), Type: "threshold", Minutes: 60, KM: 11},
		{Date: weekDate(4, 1), Type: "intervals", Minutes: 60, KM: 11},
		{Date: weekDate(4, 2), Type: "threshold", Minutes: 60, KM: 11},
		{Date: weekDate(4, 3), Type: "intervals", Minutes: 60, KM: 11},
		{Date: weekDate(4, 4), Type: "threshold", Minutes: 60, KM: 11},
		{Date: weekDate(4, 5), Type: "easy", Minutes: 60, KM: 10},
		{Date: weekDate(4, 6), Type: "easy", Minutes: 60, KM: 10},
	}
	in := WeekCheckInput{WeekRole: "build", History: hist, Plan: plan}
	it := run(t, in, "intensity")
	if it[0].Level != "WARN" {
		t.Errorf("强度占比 25.7%% 应 WARN，得 %s: %s", it[0].Level, it[0].Detail)
	}
	rs := run(t, in, "rest")
	if rs[0].Level != "WARN" {
		t.Errorf("无休息日应 WARN，得 %s", rs[0].Level)
	}
}

func TestCheckWeek_NoHistory(t *testing.T) {
	plan := []TrainingUnit{{Date: weekDate(0, 0), Type: "easy", Minutes: 40, KM: 7}}
	vols := run(t, WeekCheckInput{WeekRole: "build", Plan: plan}, "volume")
	if vols[0].Level != "WARN" {
		t.Errorf("无历史应 WARN（无法校验进展），得 %s", vols[0].Level)
	}
}

func TestCheckWeek_EmptyPlanError(t *testing.T) {
	if _, err := CheckWeek(WeekCheckInput{WeekRole: "build"}); err == nil {
		t.Fatal("空计划应报错而不是静默")
	}
}

// weekDate 返回第 w 周（从 2026-01-05 周一算起）第 d 天的日期。
var weekBase = time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)

func weekDate(w, d int) string {
	return weekBase.AddDate(0, 0, w*7+d).Format("2006-01-02")
}

func run(t *testing.T, in WeekCheckInput, rule string) []Verdict {
	t.Helper()
	vs, err := CheckWeek(in)
	if err != nil {
		t.Fatalf("CheckWeek 报错: %v", err)
	}
	var out []Verdict
	for _, v := range vs {
		if v.Rule == rule {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		t.Fatalf("规则 %s 无判定输出", rule)
	}
	return out
}

// 供金标准对照：从 JSON 文件读输入（与 check_week.py 的 simple 形状兼容）
type fileInput struct {
	WeekRole     string         `json:"week_role"`
	Cautious     bool           `json:"cautious"`
	History      []TrainingUnit `json:"history"`
	HistoryWeeks []struct {
		WeekStart string  `json:"week_start"`
		Minutes   float64 `json:"minutes"`
		KM        float64 `json:"km"`
		LongMin   float64 `json:"long_min"`
	} `json:"history_weeks"`
	Plan []TrainingUnit `json:"plan"`
}

func loadFileInput(path string) (WeekCheckInput, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return WeekCheckInput{}, err
	}
	var f fileInput
	if err := json.Unmarshal(b, &f); err != nil {
		return WeekCheckInput{}, err
	}
	in := WeekCheckInput{WeekRole: f.WeekRole, Cautious: f.Cautious, History: f.History, Plan: f.Plan}
	for _, w := range f.HistoryWeeks {
		in.HistoryWeeks = append(in.HistoryWeeks, WeekTotal{WeekStart: w.WeekStart, Minutes: w.Minutes, KM: w.KM, LongMin: w.LongMin})
	}
	return in, nil
}

// 票 02：Params 与 detail 数值一致性（结构化副本不得漂移）
func TestVerdictParams(t *testing.T) {
	// 4 周历史各 300min（周一 2025-12-15 起），计划周 2026-01-12：
	// 总量 355min=+18%（FAIL 需 >20%，用 380min 触发 FAIL）+ 背靠背硬课
	base := time.Date(2025, 12, 15, 0, 0, 0, 0, time.UTC)
	hist := []TrainingUnit{}
	for w := 0; w < 4; w++ {
		for d := 0; d < 3; d++ {
			hist = append(hist, TrainingUnit{
				Date:    base.AddDate(0, 0, w*7+d*2).Format("2006-01-02"),
				Minutes: 100, KM: 16,
			})
		}
	}
	plan := []TrainingUnit{
		{Date: "2026-01-13", Type: "intervals", Minutes: 70, KM: 13},
		{Date: "2026-01-14", Type: "tempo", Minutes: 65, KM: 12},
		{Date: "2026-01-15", Type: "long", Minutes: 245, KM: 38},
		{Date: "2026-01-16", Type: "strength", Minutes: 40},
	}
	vs, err := CheckWeek(WeekCheckInput{WeekRole: "build", History: hist, Plan: plan})
	if err != nil {
		t.Fatal(err)
	}
	byRule := map[string]Verdict{}
	for _, v := range vs {
		byRule[v.Rule] = v
	}
	// volume FAIL：detail 含 "+58%"，Params.change 必须同值
	vol := byRule["volume"]
	if vol.Params == nil {
		t.Fatalf("volume 应有 Params: %+v", vol)
	}
	if got := fmt.Sprintf("%+.0f%%", vol.Params["change"]); !strings.Contains(vol.Detail, got) {
		t.Errorf("volume detail %q 应含 Params.change 格式化值 %q", vol.Detail, got)
	}
	if vol.Params["failAt"] != 20 || vol.Params["warnAt"] != 15 {
		t.Errorf("默认阈值应 warn15/fail20，得 %v", vol.Params)
	}
	// long-share：share/limit/runs 与 detail 数字一致
	ls := byRule["long-share"]
	if ls.Params == nil {
		t.Fatal("long-share 应有 Params")
	}
	if got := fmt.Sprintf("%.0f%%", ls.Params["share"]); !strings.Contains(ls.Detail, got) {
		t.Errorf("long-share detail %q 应含 %q", ls.Detail, got)
	}
	// hard-days：count 与 detail 一致；背靠背 FAIL 带 d1/d2
	// （hard-days 可能多条——取带 count 的那条）
	var hd Verdict
	for _, v := range vs {
		if v.Rule == "hard-days" && v.Params != nil {
			hd = v
			break
		}
	}
	if got := fmt.Sprintf("%d hard days", int(hd.Params["count"])); !strings.Contains(hd.Detail, got) {
		t.Errorf("hard-days detail %q 应含 %q", hd.Detail, got)
	}
	b2b := false
	for _, v := range vs {
		if v.Rule == "hard-days" && v.ParamsStr != nil && v.ParamsStr["d1"] != "" {
			b2b = true
			if !strings.Contains(v.Detail, v.ParamsStr["d1"]) || !strings.Contains(v.Detail, v.ParamsStr["d2"]) {
				t.Errorf("背靠背 detail %q 应含日期 %s/%s", v.Detail, v.ParamsStr["d1"], v.ParamsStr["d2"])
			}
		}
	}
	if !b2b {
		t.Error("interval+hard 相邻应产生背靠背 ParamsStr")
	}
	// spike clean 标志
	sp := byRule["spike"]
	if sp.Params == nil {
		t.Fatal("spike 应有 Params")
	}
	// rest：days 与 detail 一致
	rs := byRule["rest"]
	if got := fmt.Sprintf("%d day", int(rs.Params["days"])); !strings.Contains(rs.Detail, got) {
		t.Errorf("rest detail %q 应含 %q", rs.Detail, got)
	}
}
