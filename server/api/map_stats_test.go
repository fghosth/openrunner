package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const tdFiles = "../../core/ingest/testdata/"

func TestTracksEndpoint(t *testing.T) {
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)

	// 导入 GPX（有 GPS 轨迹）+ 手工活动（无 GPS）
	if w := uploadFIT(t, h, token, "1", tdFiles+"simple.gpx"); w.Code != http.StatusCreated {
		t.Fatalf("GPX 导入失败: %d", w.Code)
	}
	if w := do(h, http.MethodPost, "/api/athletes/1/activities", token,
		`{"date":"2026-04-12","sport":"run","durationSecs":1800,"distanceM":5000,"isPlanned":false}`); w.Code != http.StatusCreated {
		t.Fatal("手工活动失败")
	}

	t.Run("返回有 GPS 的活动轨迹，无 GPS 的活动被排除", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/activities/tracks", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d: %s", w.Code, w.Body.String())
		}
		var tracks []struct {
			ID     uint         `json:"id"`
			Sport  string       `json:"sport"`
			Points [][2]float64 `json:"points"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &tracks)
		if len(tracks) != 1 {
			t.Fatalf("期望 1 条轨迹（只有 GPX 有 GPS）, 得到 %d", len(tracks))
		}
		// GPX fixture 的 5 个 trkpt 都带 lat/lon（空洞只缺 hr/海拔采样），故 5 个有效点
		if tracks[0].Sport != "run" || len(tracks[0].Points) != 5 {
			t.Fatalf("期望 run + 5 个有效点, 得到 %+v", tracks[0])
		}
		if tracks[0].Points[0][0] != 31.2304 {
			t.Errorf("期望首点 lat 31.2304, 得到 %v", tracks[0].Points[0][0])
		}
	})

	t.Run("降采样参数生效", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/activities/tracks?max=2", token, "")
		var tracks []struct {
			Points [][2]float64 `json:"points"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &tracks)
		if len(tracks) != 1 || len(tracks[0].Points) != 2 {
			t.Fatalf("期望 2 点, 得到 %+v", tracks)
		}
	})
}

func TestAnnualStatsEndpoint(t *testing.T) {
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)

	// 2026 年两次活动（GPX 4/10 + TCX 4/11），外加 2025 年一次
	if w := uploadFIT(t, h, token, "1", tdFiles+"simple.gpx"); w.Code != http.StatusCreated {
		t.Fatal(w.Code)
	}
	if w := uploadFIT(t, h, token, "1", tdFiles+"simple.tcx"); w.Code != http.StatusCreated {
		t.Fatal(w.Code)
	}
	if w := do(h, http.MethodPost, "/api/athletes/1/activities", token,
		`{"date":"2025-06-01","sport":"run","durationSecs":3600,"distanceM":10000,"isPlanned":false}`); w.Code != http.StatusCreated {
		t.Fatal("2025 活动失败")
	}

	t.Run("2026 统计只含本年活动", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/stats/annual?year=2026", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d: %s", w.Code, w.Body.String())
		}
		var s struct {
			Year           int     `json:"year"`
			ActivityCount  int     `json:"activityCount"`
			TotalDistanceM float64 `json:"totalDistanceM"`
			ActiveDays     int     `json:"activeDays"`
			Monthly        []struct {
				Month     int     `json:"month"`
				Count     int     `json:"count"`
				DistanceM float64 `json:"distanceM"`
			} `json:"monthly"`
			Streaks struct {
				Longest int `json:"longest"`
				Current int `json:"current"`
			} `json:"streaks"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &s)
		if s.Year != 2026 || s.ActivityCount != 2 || s.ActiveDays != 2 {
			t.Fatalf("期望 2026 两次活动 2 天, 得到 %+v", s)
		}
		if len(s.Monthly) != 12 {
			t.Fatalf("期望 12 月桶, 得到 %d", len(s.Monthly))
		}
		if s.Monthly[3].Count != 2 { // 4 月
			t.Errorf("期望 4 月 2 次, 得到 %+v", s.Monthly[3])
		}
		if s.Streaks.Longest != 2 { // 4/10 与 4/11 连续
			t.Errorf("期望最长连续 2（4/10-4/11）, 得到 %d", s.Streaks.Longest)
		}
	})

	t.Run("缺省年份 = 今年", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/stats/annual", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d", w.Code)
		}
		var s struct {
			Year int `json:"year"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &s)
		if s.Year < 2024 {
			t.Fatalf("期望合理年份, 得到 %d", s.Year)
		}
	})

	t.Run("非法年份 400", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/stats/annual?year=abc", token, "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("期望 400, 得到 %d", w.Code)
		}
	})
}

func TestAnnualPosterEndpoint(t *testing.T) {
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)
	if w := uploadFIT(t, h, token, "1", tdFiles+"simple.gpx"); w.Code != http.StatusCreated {
		t.Fatal(w.Code)
	}

	t.Run("返回 SVG 海报（含年份与档案名）", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/poster.svg?year=2026", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "image/svg+xml; charset=utf-8" {
			t.Errorf("期望 SVG Content-Type, 得到 %q", ct)
		}
		body := w.Body.String()
		for _, want := range []string{"<svg", "</svg>", "2026", "年度回顾", "测试档案"} {
			if !strings.Contains(body, want) {
				t.Errorf("海报缺少 %q", want)
			}
		}
	})

	t.Run("自定义强调色", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/poster.svg?year=2026&accent=%23d97706", token, "")
		if !strings.Contains(w.Body.String(), "#d97706") {
			t.Error("期望自定义强调色")
		}
	})

	t.Run("非法年份 400", func(t *testing.T) {
		if w := do(h, http.MethodGet, "/api/athletes/1/poster.svg?year=xx", token, ""); w.Code != http.StatusBadRequest {
			t.Fatalf("期望 400, 得到 %d", w.Code)
		}
	})
}
