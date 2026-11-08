package api

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/openrunner/core"
	"github.com/openrunner/server/store"
)

// 金标准对照的 Go 侧输出：把生产路径算出的 PMC 序列写成 JSON，
// 交给 scripts/golden-check.py 与「照发表公式独立实现的 Python」逐项比对。
//
// 走的是 handleAthletePMC 同一条路径（loadDailyLoads → ListSeasons →
// buildPMCInput → core.ComputePMC），不是另写一份聚合逻辑——否则验的是
// 我对聚合的第二次理解，而不是真正上线的那条路。
//
// 默认跳过，只在显式给出库路径时运行：
//
//	OPENRUNNER_GOLDEN_DB=~/.openrunner/openrunner.db OPENRUNNER_GOLDEN_ATHLETE=1 \
//	OPENRUNNER_GOLDEN_OUT=/tmp/go_pmc.json \
//	go test ./api -run TestGoldenDumpPMC -count=1
func TestGoldenDumpPMC(t *testing.T) {
	dbPath := os.Getenv("OPENRUNNER_GOLDEN_DB")
	if dbPath == "" {
		t.Skip("未设置 OPENRUNNER_GOLDEN_DB，跳过金标准对照")
	}
	athleteID := 1
	if v := os.Getenv("OPENRUNNER_GOLDEN_ATHLETE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("OPENRUNNER_GOLDEN_ATHLETE 非法: %v", err)
		}
		athleteID = n
	}
	metric := os.Getenv("OPENRUNNER_GOLDEN_METRIC")
	if metric != "rtss" {
		metric = "trimp"
	}
	out := os.Getenv("OPENRUNNER_GOLDEN_OUT")
	if out == "" {
		out = "/tmp/go_pmc.json"
	}

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("打开库失败: %v", err)
	}
	defer db.Close()
	gdb := db.Gorm()

	// 与 handler 完全一致的调用顺序与默认值
	rows, err := loadDailyLoads(gdb, uint(athleteID), metric)
	if err != nil {
		t.Fatalf("聚合失败: %v", err)
	}
	seasons, err := store.ListSeasons(gdb, uint(athleteID))
	if err != nil {
		t.Fatalf("赛季查询失败: %v", err)
	}
	today := time.Now().Local().Format("2006-01-02")
	in := buildPMCInput(today, rows, seasons)
	series := core.ComputePMC(in)

	payload := map[string]any{
		"today":      today,
		"metric":     metric,
		"athlete_id": athleteID,
		"from":       in.From,
		"to":         in.To,
		"series":     series,
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatalf("写出失败: %v", err)
	}
	t.Logf("已写出 %s：%d 天，%s 负荷，区间 %s → %s", out, len(series.Dates), metric, in.From, in.To)
}

// 把每条活动的 NP（Coggan 口径）写成 JSON，交给 golden-check.py 与独立
// Python 实现逐条比。真实数据上的交叉验证比合成序列的单元测试强得多——
// 真实功率流里有停表段、有伪影尖峰、有非 1 秒采样，这些合成用例覆盖不全。
func TestGoldenDumpNP(t *testing.T) {
	dbPath := os.Getenv("OPENRUNNER_GOLDEN_DB")
	if dbPath == "" {
		t.Skip("未设置 OPENRUNNER_GOLDEN_DB，跳过金标准对照")
	}
	athleteID := 1
	if v := os.Getenv("OPENRUNNER_GOLDEN_ATHLETE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("OPENRUNNER_GOLDEN_ATHLETE 非法: %v", err)
		}
		athleteID = n
	}
	out := os.Getenv("OPENRUNNER_GOLDEN_NP_OUT")
	if out == "" {
		out = "/tmp/go_np.json"
	}

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("打开库失败: %v", err)
	}
	defer db.Close()

	var rows []struct {
		ActivityID      uint
		Data            []byte
		RecIntervalSecs int
	}
	if err := db.Gorm().Table("activity_streams s").
		Select("s.activity_id, s.data, s.rec_interval_secs").
		Joins("JOIN activities a ON a.id = s.activity_id").
		Where("a.athlete_id = ?", athleteID).
		Scan(&rows).Error; err != nil {
		t.Fatalf("读通道失败: %v", err)
	}

	type npRow struct {
		ID     uint    `json:"id"`
		Np     float64 `json:"np"`
		N      int     `json:"n"`
		Sample int     `json:"sampleSecs"`
	}
	items := make([]npRow, 0, len(rows))
	for _, r := range rows {
		st, err := core.UnmarshalStream(r.Data)
		if err != nil || len(st.Power) == 0 {
			continue
		}
		np := core.NormalizedPower(st.Power, r.RecIntervalSecs)
		if np <= 0 {
			continue
		}
		items = append(items, npRow{ID: r.ActivityID, Np: np, N: len(st.Power), Sample: r.RecIntervalSecs})
	}
	b, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatalf("写出失败: %v", err)
	}
	t.Logf("已写出 %s：%d 条有功率的活动", out, len(items))
}

// TestGoldenDumpRtss 走生产口径（effective duration + NormalizedSpeedKph + Rtss）
// dump 跑步 rTSS，Python 从同一库的流独立重算比对（debt-sprint 票 03）。
func TestGoldenDumpRtss(t *testing.T) {
	dbPath := os.Getenv("OPENRUNNER_GOLDEN_DB")
	if dbPath == "" {
		t.Skip("未设置 OPENRUNNER_GOLDEN_DB，跳过金标准对照")
	}
	athleteID := 1
	if v := os.Getenv("OPENRUNNER_GOLDEN_ATHLETE"); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			athleteID = n
		}
	}
	out := os.Getenv("OPENRUNNER_GOLDEN_RTSS_OUT")
	if out == "" {
		out = "/tmp/go_rtss.json"
	}
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var rows []struct {
		ActivityID      uint
		StartTime       int64
		TimerSecs       int
		DurationSecs    int
		Data            []byte
		RecIntervalSecs int
	}
	if err := db.Gorm().Table("activity_streams s").
		Select("s.activity_id, a.start_time, a.timer_secs, a.duration_secs, s.data, s.rec_interval_secs").
		Joins("JOIN activities a ON a.id = s.activity_id").
		Where("a.athlete_id = ? AND a.sport = 'run'", athleteID).
		Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	type rtssRow struct {
		ID       uint    `json:"id"`
		DurSecs  float64 `json:"durSecs"`
		Ns       float64 `json:"ns"`
		ThrKph   float64 `json:"thrKph"`
		Rtss     float64 `json:"rtss"`
		N        int     `json:"n"`
		Interval int     `json:"intervalSecs"`
	}
	items := make([]rtssRow, 0, len(rows))
	for _, r := range rows {
		st, err := core.UnmarshalStream(r.Data)
		if err != nil || len(st.SpeedKph) == 0 {
			continue
		}
		dur := float64(r.TimerSecs)
		if dur <= 0 {
			dur = float64(r.DurationSecs)
		}
		date := time.Unix(r.StartTime, 0).Local().Format("2006-01-02")
		pz, err := store.PaceZoneForDate(db.Gorm(), uint(athleteID), date)
		if err != nil || pz == nil || pz.ThresholdPaceSecsPerKm <= 0 {
			continue
		}
		thrKph := 3600.0 / float64(pz.ThresholdPaceSecsPerKm)
		ns := core.NormalizedSpeedKph(st.SpeedKph)
		items = append(items, rtssRow{ID: r.ActivityID, DurSecs: dur, Ns: ns,
			ThrKph: thrKph, Rtss: core.Rtss(dur, ns, thrKph),
			N: len(st.SpeedKph), Interval: r.RecIntervalSecs})
	}
	b, _ := json.Marshal(map[string]any{"items": items})
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("已写出 %s（%d 条）", out, len(items))
}

// TestGoldenDumpWBal 用固定参数对真实骑行功率流算 W'bal，
// Python 从同一库独立重算（Skiba 2012 积分式）比对。
func TestGoldenDumpWBal(t *testing.T) {
	dbPath := os.Getenv("OPENRUNNER_GOLDEN_DB")
	if dbPath == "" {
		t.Skip("未设置 OPENRUNNER_GOLDEN_DB，跳过金标准对照")
	}
	athleteID := 1
	if v := os.Getenv("OPENRUNNER_GOLDEN_ATHLETE"); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			athleteID = n
		}
	}
	out := os.Getenv("OPENRUNNER_GOLDEN_WBAL_OUT")
	if out == "" {
		out = "/tmp/go_wbal.json"
	}
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var rows []struct {
		ActivityID      uint
		Data            []byte
		RecIntervalSecs int
	}
	if err := db.Gorm().Table("activity_streams s").
		Select("s.activity_id, s.data, s.rec_interval_secs").
		Joins("JOIN activities a ON a.id = s.activity_id").
		Where("a.athlete_id = ? AND a.sport = 'ride'", athleteID).
		Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	const cp, wprime = 280.0, 134.8
	type wbalRow struct {
		ID       uint      `json:"id"`
		Cp       float64   `json:"cp"`
		Wprime   float64   `json:"wprime"`
		Tau      float64   `json:"tau"`
		Interval int       `json:"intervalSecs"`
		N        int       `json:"n"`
		Series   []float64 `json:"series"`
	}
	items := make([]wbalRow, 0, len(rows))
	for _, r := range rows {
		st, err := core.UnmarshalStream(r.Data)
		if err != nil || len(st.Power) == 0 {
			continue
		}
		tau := core.TauFromData(cp, core.AvgPowerBelowCP(st.Power, cp))
		series := core.WBal(st.Power, cp, wprime, tau, r.RecIntervalSecs)
		// 降采样（每 10 点取 1）控制 JSON 体积；Python 侧同样按索引比对
		thin := make([]float64, 0, len(series)/10+1)
		for i, v := range series {
			if i%10 == 0 {
				thin = append(thin, v)
			}
		}
		items = append(items, wbalRow{ID: r.ActivityID, Cp: cp, Wprime: wprime, Tau: tau,
			Interval: r.RecIntervalSecs, N: len(series), Series: thin})
	}
	b, _ := json.Marshal(map[string]any{"items": items})
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("已写出 %s（%d 条）", out, len(items))
}
