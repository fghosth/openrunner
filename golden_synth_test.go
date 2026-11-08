package core

import (
	"encoding/json"
	"os"
	"testing"
)

// 金标准对照的合成用例 dump（debt-sprint 票 03）：
// Python 侧用独立实现/已知真值生成用例，这里走生产拟合路径，逐项比对。
//
//	OPENRUNNER_GS_IN=/tmp/gs_cases.json OPENRUNNER_GS_OUT=/tmp/gs_go.json \
//	go test . -run TestGoldenDumpSynth -count=1

type gsCases struct {
	CsCases   [][]EffortPoint `json:"cs_cases"`
	Cp3Cases  [][]EffortPoint `json:"cp3_cases"`
	Banister  []struct {
		Loads map[string]float64 `json:"loads"`
		Races []struct {
			Date string  `json:"date"`
			Perf float64 `json:"perf"`
		} `json:"races"`
		Tau1 float64 `json:"tau1"`
		Tau2 float64 `json:"tau2"`
	} `json:"banister"`
	WbalCases []struct {
		Power    []float64 `json:"power"`
		Cp       float64   `json:"cp"`
		Wprime   float64   `json:"wprime"`
		Tau      float64   `json:"tau"`
		Interval int       `json:"interval"`
	} `json:"wbal_cases"`
}

func TestGoldenDumpSynth(t *testing.T) {
	in := os.Getenv("OPENRUNNER_GS_IN")
	if in == "" {
		t.Skip("未设置 OPENRUNNER_GS_IN，跳过合成金标准 dump")
	}
	out := os.Getenv("OPENRUNNER_GS_OUT")
	if out == "" {
		out = "/tmp/gs_go.json"
	}
	b, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}
	var c gsCases
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}

	type csRow struct{ Cs, Dprime, R2 float64 }
	cs := make([]csRow, 0, len(c.CsCases))
	for _, pts := range c.CsCases {
		v, d, q := FitCriticalSpeed(pts)
		cs = append(cs, csRow{v, d, q.R2})
	}
	type cp3Row struct{ Cs, Dprime, Tau, Peak float64 }
	cp3 := make([]cp3Row, 0, len(c.Cp3Cases))
	for _, pts := range c.Cp3Cases {
		r := FitCP3(pts)
		cp3 = append(cp3, cp3Row{r.CS, r.DPrime, r.Tau, r.Peak})
	}
	type banRow struct{ P0, K1, K2, R2 float64 }
	ban := make([]banRow, 0, len(c.Banister))
	for _, bc := range c.Banister {
		races := make([]RacePoint, 0, len(bc.Races))
		for _, r := range bc.Races {
			races = append(races, RacePoint{Date: r.Date, Performance: r.Perf})
		}
		var m BanisterModel
		if bc.Tau1 > 0 {
			m = FitBanisterWithTau(bc.Loads, races, bc.Tau1, bc.Tau2)
		} else {
			m = FitBanister(bc.Loads, races)
		}
		ban = append(ban, banRow{m.P0, m.K1, m.K2, m.R2})
	}
	wbal := make([][]float64, 0, len(c.WbalCases))
	for _, wc := range c.WbalCases {
		wbal = append(wbal, WBal(wc.Power, wc.Cp, wc.Wprime, wc.Tau, wc.Interval))
	}

	res := map[string]any{"cs": cs, "cp3": cp3, "banister": ban, "wbal": wbal}
	bb, _ := json.Marshal(res)
	if err := os.WriteFile(out, bb, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("已写出 %s", out)
}
