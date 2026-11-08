package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/openrunner/server/store"
)

// coach week-check 端点测试（票 02）。

func nextMonday() time.Time {
	now := time.Now()
	off := (int(now.Weekday()) + 6) % 7
	return now.AddDate(0, 0, -off+7)
}

func insertRun(t *testing.T, s *store.DB, aid uint, when time.Time, mins float64, km float64, planned bool) {
	t.Helper()
	a := &store.ActivityModel{
		AthleteID: aid, Sport: "run",
		StartTime: when.Unix(), DurationSecs: int(mins * 60), TimerSecs: int(mins * 60),
		DistanceM: km * 1000, IsPlanned: planned,
	}
	if err := s.Gorm().Create(a).Error; err != nil {
		t.Fatal(err)
	}
}

func TestCoachWeekCheck(t *testing.T) {
	h, db := newTestServer(t)
	token := setupAdmin(t, h)
	aid := createAthlete(t, h, token)

	// 4 周历史：每周 3 课 × 100min / 16km
	base := time.Now()
	for w := 1; w <= 4; w++ {
		for d := 0; d < 3; d++ {
			when := base.AddDate(0, 0, -w*7-d*2)
			insertRun(t, db, aid, when, 100, 16, false)
		}
	}

	t.Run("正常计划 → 判定列表", func(t *testing.T) {
		nm := nextMonday()
		body := `{"week_role":"build","plan":[
			{"date":"` + nm.Format("2006-01-02") + `","type":"easy","minutes":100,"km":16},
			{"date":"` + nm.AddDate(0, 0, 2).Format("2006-01-02") + `","type":"easy","minutes":100,"km":16},
			{"date":"` + nm.AddDate(0, 0, 4).Format("2006-01-02") + `","type":"long","minutes":100,"km":16}]}`
		w := do(h, http.MethodPost, "/api/athletes/1/coach/week-check", token, body)
		if w.Code != 200 {
			t.Fatalf("得 %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Verdicts []struct {
				Level, Rule, Detail, Evidence string
			}
			Week map[string]any
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Verdicts) < 5 {
			t.Fatalf("判定过少: %+v", resp.Verdicts)
		}
		var vol *struct{ Level, Rule string }
		for i := range resp.Verdicts {
			if resp.Verdicts[i].Rule == "volume" {
				vol = &struct{ Level, Rule string }{resp.Verdicts[i].Level, resp.Verdicts[i].Rule}
			}
		}
		if vol == nil || vol.Level != "OK" {
			t.Errorf("与历史持平应 volume OK，得 %+v", vol)
		}
	})

	t.Run("超量计划 → FAIL 存在", func(t *testing.T) {
		nm := nextMonday()
		body := `{"plan":[{"date":"` + nm.Format("2006-01-02") +
			`","type":"long","minutes":300,"km":48}]}`
		w := do(h, http.MethodPost, "/api/athletes/1/coach/week-check", token, body)
		var resp struct {
			Verdicts []map[string]string
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		fail := false
		for _, v := range resp.Verdicts {
			if v["level"] == "FAIL" {
				fail = true
			}
		}
		if !fail {
			t.Errorf("300min/48km 单课应触发 FAIL: %+v", resp.Verdicts)
		}
	})

	t.Run("非法 week_role → 400", func(t *testing.T) {
		w := do(h, http.MethodPost, "/api/athletes/1/coach/week-check", token,
			`{"week_role":"yolo","plan":[{"date":"2026-01-05","minutes":30}]}`)
		if w.Code != 400 {
			t.Errorf("期望 400，得 %d", w.Code)
		}
	})

	t.Run("无计划无日历 → 400", func(t *testing.T) {
		w := do(h, http.MethodPost, "/api/athletes/1/coach/week-check", token, `{}`)
		if w.Code != 400 {
			t.Errorf("期望 400，得 %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("计划缺省取日历", func(t *testing.T) {
		nm := nextMonday()
		// 生产形态：手工创建的课 start_time 是本地午夜（回归守卫，别改回 +9h）
		insertRun(t, db, aid, time.Date(nm.Year(), nm.Month(), nm.Day(), 0, 0, 0, 0, time.Local), 60, 10, true)
		w := do(h, http.MethodPost, "/api/athletes/1/coach/week-check", token, `{}`)
		if w.Code != 200 {
			t.Fatalf("期望 200，得 %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Week map[string]any
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if runs, _ := resp.Week["runs"].(float64); runs != 1 {
			t.Errorf("期望从日历取到 1 课，week=%+v", resp.Week)
		}
	})

	t.Run("未鉴权 → 401", func(t *testing.T) {
		w := do(h, http.MethodPost, "/api/athletes/1/coach/week-check", "", `{}`)
		if w.Code != 401 {
			t.Errorf("期望 401，得 %d", w.Code)
		}
	})
}

// —— 票 04：训练配速表 ——

func TestCoachPaces(t *testing.T) {
	h, db := newTestServer(t)
	token := setupAdmin(t, h)
	aid := createAthlete(t, h, token)

	t.Run("无阈值无比赛 → error 说明", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/coach/paces", token, "")
		if w.Code != 200 {
			t.Fatalf("得 %d", w.Code)
		}
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if e, _ := resp["error"].(string); e == "" {
			t.Errorf("空库应返回 error 说明，得 %+v", resp)
		}
	})

	t.Run("比赛成绩反推阈值 + VDOT", func(t *testing.T) {
		if err := db.Gorm().Create(&store.RaceResultModel{
			AthleteID: aid, Date: "2026-01-10", DistanceM: 10000, TimeSecs: 2400,
		}).Error; err != nil {
			t.Fatal(err)
		}
		w := do(h, http.MethodGet, "/api/athletes/1/coach/paces", token, "")
		var resp struct {
			Threshold map[string]any `json:"threshold"`
			Vdot      map[string]any `json:"vdot"`
			Zones     []any          `json:"zones8020"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Threshold["source"] != "race" {
			t.Errorf("阈值应来自比赛反推，得 %+v", resp.Threshold)
		}
		if vd, _ := resp.Vdot["value"].(float64); vd < 51.5 || vd > 52.4 {
			t.Errorf("10K 40:00 应 VDOT≈51.9，得 %v", resp.Vdot["value"])
		}
		if len(resp.Zones) != 7 {
			t.Errorf("80/20 应 7 区，得 %d", len(resp.Zones))
		}
	})

	t.Run("pace_zones 优先 + LTHR 心率列", func(t *testing.T) {
		if err := db.Gorm().Create(&store.PaceZoneModel{
			AthleteID: aid, ValidFrom: "2000-01-01", ValidTo: "9999-12-31",
			ThresholdPaceSecsPerKm: 280,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Gorm().Create(&store.HrZoneModel{
			AthleteID: aid, ValidFrom: "2000-01-01", ValidTo: "9999-12-31",
			MaxHr: 190, RestHr: 60, LtHr: 170,
		}).Error; err != nil {
			t.Fatal(err)
		}
		w := do(h, http.MethodGet, "/api/athletes/1/coach/paces", token, "")
		var resp struct {
			Threshold map[string]any `json:"threshold"`
			Lthr      float64        `json:"lthr"`
			Zones     []struct {
				Name string   `json:"name"`
				HRHi *float64 `json:"hrHi"`
			} `json:"zones8020"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Threshold["source"] != "pace_zones" {
			t.Errorf("pace_zones 应优先，得 %+v", resp.Threshold)
		}
		if resp.Lthr != 165 {
			t.Errorf("lthr 应 165，得 %v", resp.Lthr)
		}
		if resp.Zones[0].HRHi == nil || *resp.Zones[0].HRHi <= 0 {
			t.Error("有 LTHR 时心率列应填充")
		}
	})
}

// —— 票 05：全马多模型预测 ——

func TestCoachPredict(t *testing.T) {
	h, db := newTestServer(t)
	token := setupAdmin(t, h)
	aid := createAthlete(t, h, token)

	t.Run("无基准 → error", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/coach/predict", token, "")
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if e, _ := resp["error"].(string); e == "" {
			t.Errorf("无成绩无参数应返回 error，得 %+v", resp)
		}
	})

	// 基准 10K 48:48 + 近 8 周每周 40km/4h 跑量
	if err := db.Gorm().Create(&store.RaceResultModel{
		AthleteID: aid, Date: "2026-01-10", DistanceM: 10000, TimeSecs: 2928,
	}).Error; err != nil {
		t.Fatal(err)
	}
	for w := 1; w <= 8; w++ {
		for d := 0; d < 4; d++ {
			insertRun(t, db, aid, time.Now().AddDate(0, 0, -w*7-d), 60, 10, false)
		}
	}

	t.Run("比赛基准 → 多模型 + 最慢推荐", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/coach/predict", token, "")
		var resp struct {
			Base          map[string]any `json:"base"`
			WeeklyKm      float64        `json:"weekly_km"`
			WeeksObserved int            `json:"weeks_observed"`
			Prediction    struct {
				Riegel      float64 `json:"riegel"`
				Vickers1    float64 `json:"vickers1"`
				Tanda       float64 `json:"tanda"`
				Recommended float64 `json:"recommended"`
			} `json:"prediction"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Base["source"] != "race" {
			t.Errorf("缺省基准应取比赛，得 %+v", resp.Base)
		}
		if resp.Prediction.Riegel <= 0 || resp.Prediction.Vickers1 <= 0 {
			t.Errorf("riegel/vickers1 应有值: %+v", resp.Prediction)
		}
		if resp.Prediction.Recommended < resp.Prediction.Riegel {
			t.Errorf("recommended 应是最慢值 ≥ Riegel: %+v", resp.Prediction)
		}
		if resp.WeeklyKm < 35 || resp.WeeklyKm > 45 {
			t.Errorf("周跑量应 ≈40km，得 %v", resp.WeeklyKm)
		}
	})

	t.Run("query 参数覆盖基准", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/coach/predict?distance=21.0975&time=1:47:40", token, "")
		var resp struct {
			Base map[string]any `json:"base"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Base["source"] != "query" {
			t.Errorf("query 应覆盖比赛基准，得 %+v", resp.Base)
		}
		if km, _ := resp.Base["distanceM"].(float64); km < 21000 || km > 21200 {
			t.Errorf("基准距离应 21.0975km，得 %v", km)
		}
	})

	t.Run("非法参数 → 400", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/coach/predict?distance=abc", token, "")
		if w.Code != 400 {
			t.Errorf("期望 400，得 %d", w.Code)
		}
	})
}
