package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestDashboardAndPersonalBest(t *testing.T) {
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)

	t.Run("无活动时返回零值而非报错", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/dashboard", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d: %s", w.Code, w.Body.String())
		}
		var d struct {
			Year   int   `json:"year"`
			Daily  []any `json:"daily"`
			Totals struct {
				All struct {
					Count int `json:"count"`
				} `json:"all"`
			} `json:"totals"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
			t.Fatalf("响应不是合法 JSON: %v", err)
		}
		if d.Year == 0 {
			t.Fatal("year 不应为 0")
		}
		if d.Totals.All.Count != 0 {
			t.Fatalf("空档案的活动数应为 0, 得到 %d", d.Totals.All.Count)
		}
	})

	t.Run("个人最佳在无速度流时返回空数组", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/personal-best", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d: %s", w.Code, w.Body.String())
		}
		var out []any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("响应不是合法 JSON 数组: %v", err)
		}
		if len(out) != 0 {
			t.Fatalf("没有跑步数据时应返回空数组, 得到 %d 项", len(out))
		}
	})

	t.Run("档案不存在返回 404", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/999/dashboard", token, "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("期望 404, 得到 %d", w.Code)
		}
	})
}
