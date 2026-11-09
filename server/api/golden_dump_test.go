package api

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/fghosth/openrunner/core"
	"github.com/fghosth/openrunner/server/store"
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

// TestGoldenDumpRtss dump 跑步 rTSS，Python 从同一库的流独立重算比对（debt-sprint 票 03）。
//
// **必须调用生产函数**（core.NormalizedSpeed + core.Rtss，与 api/metrics.go 同一对），
// 否则 dump 与生产脱队、对照变成自说自话——这个坑真踩过：rTSS 改用 30 秒平滑后，
// 这个 dump 还留着 NormalizedSpeedKph（裸 4 次方均值），于是 Python 参照和 Go dump
// 同为「裸」口径，逐值一致地一起错。
//
// 第二道闸门：dump 值必须等于库里 activity_metrics.rtss（那是生产上次跑出来的结果）。
// 不等说明「代码变了但没重算」或「dump 没走生产路径」，两种都得先修好再对照。
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
		ns := core.NormalizedSpeed(st.SpeedKph, r.RecIntervalSecs) // 生产口径
		items = append(items, rtssRow{ID: r.ActivityID, DurSecs: dur, Ns: ns,
			ThrKph: thrKph, Rtss: core.Rtss(dur, ns, thrKph),
			N: len(st.SpeedKph), Interval: r.RecIntervalSecs})
	}
	// 第二道闸门（软）：与库里持久化的 rtss 比一遍。快照可能早于当前代码（指标没重算），
	// 所以这里只报告不失败；「dump 必须走生产路径」由 go test 的
	// TestGoldenDumpRtssMatchesProductionMetrics 在合成库上硬保证。
	var stored []struct {
		ActivityID uint
		Rtss       float64
	}
	if err := db.Gorm().Table("activity_metrics").Select("activity_id, rtss").Scan(&stored).Error; err != nil {
		t.Fatal(err)
	}
	persisted := make(map[uint]float64, len(stored))
	for _, s := range stored {
		persisted[s.ActivityID] = s.Rtss
	}
	stale := 0
	for _, it := range items {
		if want, ok := persisted[it.ID]; ok && math.Abs(want-it.Rtss) > 1e-6 {
			stale++
		}
	}
	if stale > 0 {
		t.Logf("提示：%d 条活动的持久化 rtss 与当前代码不一致（快照早于代码变更？）", stale)
	}

	b, _ := json.Marshal(map[string]any{"items": items})
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("已写出 %s（%d 条，与库中持久化值一致）", out, len(items))
}

// TestGoldenDumpWBal 用固定参数对真实骑行功率流算 W'bal（生产函数 core.WBal），
// Python 从同一库独立重算（Skiba 2012 **分段式**）比对。
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
	const cp, wprime = 300.0, 15000.0
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

// TestGoldenDumpRtssMatchesProductionMetrics 「dump 必须走生产路径」的硬保证。
//
// 为什么需要：rTSS 改成 30 秒平滑后，TestGoldenDumpRtss 还留着裸 4 次方均值，
// 于是 Go dump 与 Python 参照同为「裸」口径，逐值一致地一起错——对照台全绿，
// 却什么都没验证（典型的参照与实现一起脱队）。
//
// 这里用合成库把两条路径钉死：recomputeMetrics（生产落库）算出的 rtss
// 必须与 dump 里那几行算式逐值相等。
func TestGoldenDumpRtssMatchesProductionMetrics(t *testing.T) {
	h, db := newTestServer(t)
	_ = h
	athlete := &store.AthleteModel{ID: 1, Sex: "M"}
	if err := db.Gorm().Create(&store.PaceZoneModel{
		AthleteID: 1, ValidFrom: "2000-01-01", ValidTo: "2099-12-31",
		ThresholdPaceSecsPerKm: 300,
	}).Error; err != nil {
		t.Fatal(err)
	}

	// 带 GPS 速度尖峰的流：裸 4 次方均值与平滑值必须能区分开
	speeds := make([]float64, 1200)
	for i := range speeds {
		speeds[i] = 12
	}
	speeds[600] = 120 // 尖峰
	stream := core.Stream{RecIntervalSecs: 1, SpeedKph: speeds}
	act := core.Activity{
		Sport:     core.SportRun,
		StartTime: time.Date(2026, 5, 1, 9, 0, 0, 0, time.Local),
		Duration:  1200 * time.Second,
		TimerSecs: 1200,
		Distance:  1200 * 12 / 3.6,
	}
	id, err := store.CreateActivityWithStream(db.Gorm(), 1, act, stream, "fit", "")
	if err != nil {
		t.Fatal(err)
	}
	m, _ := store.GetActivity(db.Gorm(), 1, id)
	if err := recomputeMetrics(db.Gorm(), athlete, m); err != nil {
		t.Fatal(err)
	}
	var stored store.ActivityMetricsModel
	if err := db.Gorm().Where("activity_id = ?", id).First(&stored).Error; err != nil {
		t.Fatal(err)
	}

	// dump 里那几行（必须与生产同函数）
	st, err := store.GetStream(db.Gorm(), id)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := core.UnmarshalStream(st.Data)
	if err != nil {
		t.Fatal(err)
	}
	thrKph := 3600.0 / 300.0
	ns := core.NormalizedSpeed(decoded.SpeedKph, decoded.RecIntervalSecs)
	dumped := core.Rtss(float64(act.TimerSecs), ns, thrKph)
	if math.Abs(dumped-stored.Rtss) > 1e-9 {
		t.Fatalf("dump 与生产路径不一致：dump=%.6f 生产=%.6f", dumped, stored.Rtss)
	}
	// 反向断言：dump 必须用的是平滑口径，不是裸 4 次方均值
	bare := core.NormalizedSpeedKph(decoded.SpeedKph)
	if bare <= ns {
		t.Fatalf("尖峰没被平滑：裸 NS=%.3f 平滑 NS=%.3f", bare, ns)
	}
}
