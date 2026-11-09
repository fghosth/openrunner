package api

import (
	"encoding/json"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/openrunner/core"
	"github.com/openrunner/server/store"
)

// 计划课即便带流（导入路径可以造出来）也不得进个人最佳 / 轨迹墙。
// 年度统计早已过滤（loadDayActivities），这两条路径曾漏掉（Round 41 审计缺口）。
func TestPlannedActivityExcludedFromPBAndTracks(t *testing.T) {
	h, db := newTestServer(t)
	token := setupAdmin(t, h)
	_ = createAthlete(t, h, token)
	athlete, err := store.GetAthlete(db.Gorm(), 1)
	if err != nil {
		t.Fatal(err)
	}

	// 1 秒采样、14 km/h × 400s ≈ 1.55 km，带 GPS
	stream := core.Stream{RecIntervalSecs: 1}
	for i := 0; i < 400; i++ {
		stream.SpeedKph = append(stream.SpeedKph, 14)
		stream.DistanceM = append(stream.DistanceM, float64(i)*14/3.6)
		stream.Latitude = append(stream.Latitude, 31.2+float64(i)*1e-5)
		stream.Longitude = append(stream.Longitude, 121.5+float64(i)*1e-5)
	}
	act := core.Activity{
		Sport:     core.SportRun,
		StartTime: time.Date(2026, 1, 5, 8, 0, 0, 0, time.Local),
		Duration:  400 * time.Second,
		TimerSecs: 400,
		Distance:  stream.DistanceM[len(stream.DistanceM)-1],
		IsPlanned: true, // 计划课 + 带流 = 导入路径能造出来的形态
	}
	id, err := store.CreateActivityWithStream(db.Gorm(), 1, act, stream, "manual", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := recomputeMetrics(db.Gorm(), athlete, &store.ActivityModel{ID: id, AthleteID: 1}); err != nil {
		t.Fatal(err)
	}

	assertPB := func(want bool) {
		t.Helper()
		w := do(h, http.MethodGet, "/api/athletes/1/personal-best", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("personal-best %d", w.Code)
		}
		var best []map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &best)
		got := false
		for _, b := range best {
			if s, ok := b["seconds"].(float64); ok && s > 0 {
				got = true
			}
		}
		if got != want {
			t.Fatalf("个人最佳是否计入 = %v, 期望 %v", got, want)
		}
	}
	assertTracks := func(want int) {
		t.Helper()
		w := do(h, http.MethodGet, "/api/athletes/1/activities/tracks", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("tracks %d", w.Code)
		}
		var tracks []map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &tracks)
		if len(tracks) != want {
			t.Fatalf("轨迹墙条数 = %d, 期望 %d", len(tracks), want)
		}
	}

	assertPB(false)
	assertTracks(0)

	// 改成实际活动 → 两条路径都应看见它
	if err := db.Gorm().Exec("UPDATE activities SET is_planned = 0 WHERE id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	assertPB(true)
	assertTracks(1)
}

// 周跑量均值必须除以窗口内的完整周数（含零跑量周），不是「有跑量的周数」。
// 1 个空周会让旧口径把周跑量放大 12.5%，Tanda 全马预测偏快约 2.3 分钟。
func TestWeeklyVolumeDenominator(t *testing.T) {
	h, db := newTestServer(t)
	token := setupAdmin(t, h)
	_ = createAthlete(t, h, token)
	if w := do(h, http.MethodPut, "/api/athletes/1/pace-zones", token,
		`{"validFrom":"2000-01-01","validTo":"2099-12-31","thresholdPaceSecsPerKm":280}`); w.Code != http.StatusCreated {
		t.Fatalf("pace zone %d", w.Code)
	}
	// 一场比赛（预测基准）+ 9 周里只有 3 周有 20 km 跑量，其余 6 周休息
	if w := do(h, http.MethodPost, "/api/athletes/1/race-results", token,
		`{"date":"2026-06-06","distanceM":10000,"timeSecs":2400,"name":"10K"}`); w.Code >= 400 {
		t.Fatalf("race %d", w.Code)
	}
	monday := mondayOf(time.Now())
	for _, wk := range []int{1, 4, 7} { // 第 -1、-4、-7 周（都在 -9..-1 窗口内）
		d := monday.AddDate(0, 0, -7*wk)
		body := `{"date":"` + d.Format("2006-01-02") + `","sport":"run","durationSecs":3600,"distanceM":20000,"avgHr":150}`
		if w := do(h, http.MethodPost, "/api/athletes/1/activities", token, body); w.Code != http.StatusCreated {
			t.Fatalf("activity %d: %s", w.Code, w.Body.String())
		}
	}
	in, ok := predictInputsFromDB(db.Gorm(), 1)
	if !ok {
		t.Fatal("预测输入应可用（有比赛基准）")
	}
	// 总跑量 60 km / 9 周 = 6.67；旧口径 60/3 = 20
	if math.Abs(in.WeeklyKm-60.0/9) > 0.05 {
		t.Fatalf("周跑量 = %.2f, 期望 %.2f（分母应为 9 周而不是 3 个有跑的周）", in.WeeklyKm, 60.0/9)
	}
	if in.WeeksObserved != 9 {
		t.Fatalf("样本周数 = %d, 期望 9", in.WeeksObserved)
	}
}

// PMC 支持 metric=power：纯功率骑行（无心率带）只有 PowerLoad 算得出负荷。
func TestPMCUsesPowerLoad(t *testing.T) {
	h, db := newTestServer(t)
	token := setupAdmin(t, h)
	_ = createAthlete(t, h, token)
	if err := db.Gorm().Create(&store.CsZoneModel{
		AthleteID: 1, Domain: "power", ValidFrom: "2000-01-01", ValidTo: "2099-12-31",
		Cs: 250, Dprime: 20000, Source: "manual",
	}).Error; err != nil {
		t.Fatal(err)
	}
	athlete, _ := store.GetAthlete(db.Gorm(), 1)
	act := core.Activity{
		Sport:     core.SportRide,
		StartTime: time.Date(2026, 5, 1, 9, 0, 0, 0, time.Local), // 无心率，只有功率
		Duration:  time.Hour,
		TimerSecs: 3600,
		Distance:  30000,
	}
	power := make([]float64, 3600)
	for i := range power {
		power[i] = 250
	}
	id, err := store.CreateActivityWithStream(db.Gorm(), 1, act, core.Stream{RecIntervalSecs: 1, Power: power}, "manual", "")
	if err != nil {
		t.Fatal(err)
	}
	m, _ := store.GetActivity(db.Gorm(), 1, id)
	if err := recomputeMetrics(db.Gorm(), athlete, m); err != nil {
		t.Fatal(err)
	}

	// PMC 序列延伸到「今天+365」，末位早已衰减到 0 —— 要取活动当日的 CTL。
	get := func(metric string) float64 {
		w := do(h, http.MethodGet, "/api/athletes/1/pmc?metric="+metric, token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("pmc %s: %d", metric, w.Code)
		}
		var s struct {
			Dates []string  `json:"dates"`
			Lts   []float64 `json:"lts"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &s)
		for i, d := range s.Dates {
			if d == "2026-05-01" {
				return s.Lts[i]
			}
		}
		t.Fatalf("PMC 序列里没有 2026-05-01（共 %d 天）", len(s.Dates))
		return 0
	}
	if got := get("power"); got < 1 {
		t.Fatalf("metric=power 当日 CTL = %.2f，功率负荷没进 PMC", got)
	}
	if got := get("trimp"); got != 0 {
		t.Fatalf("无心率活动在 metric=trimp 下应为 0，得到 %.2f", got)
	}
}
