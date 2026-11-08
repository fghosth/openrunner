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
