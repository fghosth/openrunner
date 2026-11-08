package coach

import (
	"encoding/json"
	"os"
	"testing"
)

// 票 04/05 的金标准 dump：读 OPENRUNNER_COACH_PACES_IN 的用例 JSON，
// 计算配速表/VDOT/预测写成 JSON，由 coach-check.py 与 zones.py/paces.py 比对。

type pacesCases struct {
	ZoneCases []struct {
		ThresholdS float64 `json:"threshold_s"`
		LTHR       float64 `json:"lthr"`
	} `json:"zone_cases"`
	DanielsCases []struct {
		Km   float64 `json:"km"`
		Secs float64 `json:"secs"`
	} `json:"daniels_cases"`
	RaceThrCases []struct {
		Km   float64 `json:"km"`
		Secs float64 `json:"secs"`
	} `json:"race_thr_cases"`
	PredictCases []struct {
		BaseKm           float64 `json:"base_km"`
		BaseSecs         float64 `json:"base_secs"`
		Race2Km          float64 `json:"race2_km"`
		Race2Secs        float64 `json:"race2_secs"`
		WeeklyKm         float64 `json:"weekly_km"`
		TrainingPaceSecs float64 `json:"training_pace_secs"`
		WeeksObserved    int     `json:"weeks_observed"`
	} `json:"predict_cases"`
}

func TestCoachDumpPaces(t *testing.T) {
	in := os.Getenv("OPENRUNNER_COACH_PACES_IN")
	if in == "" {
		t.Skip("未设置 OPENRUNNER_COACH_PACES_IN，跳过配速对照 dump")
	}
	out := os.Getenv("OPENRUNNER_COACH_PACES_OUT")
	if out == "" {
		out = "/tmp/coach_go_paces.json"
	}
	b, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}
	var c pacesCases
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}

	zones := make([][]PaceZoneRow, 0, len(c.ZoneCases))
	for _, z := range c.ZoneCases {
		zones = append(zones, SpeedZoneTable(z.ThresholdS, z.LTHR))
	}
	daniels := make([]map[string]any, 0, len(c.DanielsCases))
	for _, d := range c.DanielsCases {
		vd, rows := Daniels(d.Km, d.Secs)
		daniels = append(daniels, map[string]any{"vdot": vd, "rows": rows})
	}
	raceThr := make([]float64, 0, len(c.RaceThrCases))
	for _, r := range c.RaceThrCases {
		raceThr = append(raceThr, ThresholdFromRace(r.Km, r.Secs))
	}
	predicts := make([]MarathonPrediction, 0, len(c.PredictCases))
	for _, p := range c.PredictCases {
		predicts = append(predicts, PredictMarathon(PredictInput{
			BaseKm: p.BaseKm, BaseSecs: p.BaseSecs,
			Race2Km: p.Race2Km, Race2Secs: p.Race2Secs,
			WeeklyKm: p.WeeklyKm, TrainingPaceSecs: p.TrainingPaceSecs,
			WeeksObserved: p.WeeksObserved,
		}))
	}

	result := map[string]any{
		"zone_cases": zones, "daniels_cases": daniels,
		"race_thr": raceThr, "predict": predicts,
	}
	bb, err := json.MarshalIndent(result, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, bb, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("已写出 %s", out)
}

// —— 票 06：单课分析 dump ——

type analysisCases struct {
	Cases []struct {
		ID        uint      `json:"id"`
		DurationS float64   `json:"durationSecs"`
		Interval  int       `json:"intervalSecs"`
		HR        []float64 `json:"hr"`
		SpeedKph  []float64 `json:"speed"`
	} `json:"cases"`
}

func TestCoachDumpAnalysis(t *testing.T) {
	in := os.Getenv("OPENRUNNER_COACH_ANALYSIS_IN")
	if in == "" {
		t.Skip("未设置 OPENRUNNER_COACH_ANALYSIS_IN，跳过单课分析对照 dump")
	}
	out := os.Getenv("OPENRUNNER_COACH_ANALYSIS_OUT")
	if out == "" {
		out = "/tmp/coach_go_analysis.json"
	}
	b, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}
	var c analysisCases
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	results := make([]SessionAnalysis, 0, len(c.Cases))
	for _, cs := range c.Cases {
		results = append(results, AnalyzeSession(cs.DurationS, cs.Interval, cs.HR, cs.SpeedKph))
	}
	bb, _ := json.MarshalIndent(results, "", " ")
	if err := os.WriteFile(out, bb, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("已写出 %s", out)
}
