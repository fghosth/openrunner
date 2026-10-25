package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Round 8 BLOCK 修复回归（每条都对应一个真实缺陷，锁住防复发）。

func TestMultipleManualActivitiesSameAthlete(t *testing.T) {
	// P0①：手工活动 content_hash 必须为 NULL——空串会撞 (athlete_id,'') 唯一索引
	// （SQLite partial index 放行空串、MySQL 普通唯一索引不容忍 → 生产 500）
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)

	for i, date := range []string{"2026-05-01", "2026-05-02", "2026-05-03"} {
		w := do(h, http.MethodPost, "/api/athletes/1/activities", token,
			`{"date":"`+date+`","sport":"run","durationSecs":3600,"distanceM":10000,"isPlanned":true}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("第 %d 个手工活动失败（content_hash 未 NULL？）: %d %s", i+1, w.Code, w.Body.String())
		}
	}

	// 无指纹活动不参与查重
	w := do(h, http.MethodGet, "/api/athletes/1/activities", token, "")
	var list []any
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 3 {
		t.Fatalf("期望 3 个活动, 得到 %d", len(list))
	}
}

func TestPosterAccentInjection(t *testing.T) {
	// P1②：accent 直入 SVG 属性 → 必须白名单，防 <script> 注入
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)

	t.Run("合法 hex 通过", func(t *testing.T) {
		for _, ok := range []string{"%23d97706", "%23f80", "%23ABCDEF"} {
			w := do(h, http.MethodGet, "/api/athletes/1/poster.svg?accent="+ok, token, "")
			if w.Code != http.StatusOK {
				t.Errorf("%s 应通过, 得到 %d", ok, w.Code)
			}
		}
	})

	t.Run("注入与非法值被 400 拒绝", func(t *testing.T) {
		for _, bad := range []string{
			"%22%2F%3E%3Cscript%3Ealert(1)%3C%2Fscript%3E", // "/><script>
			"red",
			"%23gg0000",
		} {
			w := do(h, http.MethodGet, "/api/athletes/1/poster.svg?accent="+bad, token, "")
			if w.Code != http.StatusBadRequest {
				t.Errorf("%q 应被拒绝, 得到 %d", bad, w.Code)
			}
		}
	})
}

func TestAnnualStatsExcludesPlanned(t *testing.T) {
	// P2：计划活动不计入年度距离/次数/连续天数
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)

	// 实际 1 次 + 计划 1 次
	if w := do(h, http.MethodPost, "/api/athletes/1/activities", token,
		`{"date":"2026-03-01","sport":"run","durationSecs":3600,"distanceM":10000,"isPlanned":false}`); w.Code != http.StatusCreated {
		t.Fatal(w.Code)
	}
	if w := do(h, http.MethodPost, "/api/athletes/1/activities", token,
		`{"date":"2026-03-02","sport":"run","durationSecs":3600,"distanceM":20000,"isPlanned":true}`); w.Code != http.StatusCreated {
		t.Fatal(w.Code)
	}

	w := do(h, http.MethodGet, "/api/athletes/1/stats/annual?year=2026", token, "")
	var s struct {
		ActivityCount  int     `json:"activityCount"`
		TotalDistanceM float64 `json:"totalDistanceM"`
		Streaks        struct {
			Longest int `json:"longest"`
		} `json:"streaks"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &s)
	if s.ActivityCount != 1 || s.TotalDistanceM != 10000 {
		t.Fatalf("期望只统计实际活动（1 次/10000m）, 得到 %+v", s)
	}
	if s.Streaks.Longest != 1 {
		t.Errorf("期望连续 1 天（计划日不计）, 得到 %d", s.Streaks.Longest)
	}
}

func TestNoExternalStaticResources(t *testing.T) {
	// P1⑤：内嵌 UI 不得引用外部静态资源（票 13 验收）
	h, _ := newTestServer(t)
	w := do(h, http.MethodGet, "/", "", "")
	body := w.Body.String()
	for _, bad := range []string{"fonts.googleapis.com", "fonts.gstatic.com"} {
		if strings.Contains(body, bad) {
			t.Errorf("内嵌 index.html 仍引用外部字体 %q", bad)
		}
	}
}
