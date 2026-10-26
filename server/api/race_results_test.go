package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestRaceResultsCRUD(t *testing.T) {
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)

	var created struct {
		ID uint `json:"id"`
	}

	t.Run("创建成绩", func(t *testing.T) {
		w := do(h, http.MethodPost, "/api/athletes/1/race-results", token,
			`{"date":"2026-05-01","distanceM":5000,"timeSecs":1350,"name":"春季 5K","note":"PB"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("期望 201, 得到 %d: %s", w.Code, w.Body.String())
		}
		_ = json.Unmarshal(w.Body.Bytes(), &created)
		if created.ID == 0 {
			t.Fatal("应返回 id")
		}
	})

	t.Run("列表按日期倒序", func(t *testing.T) {
		_ = do(h, http.MethodPost, "/api/athletes/1/race-results", token,
			`{"date":"2026-08-01","distanceM":10000,"timeSecs":2900}`)
		w := do(h, http.MethodGet, "/api/athletes/1/race-results", token, "")
		var list []struct {
			Date string `json:"date"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &list)
		if len(list) != 2 || list[0].Date != "2026-08-01" {
			t.Fatalf("期望 2 条且最新在前, 得到 %+v", list)
		}
	})

	t.Run("非法输入被拒", func(t *testing.T) {
		cases := []string{
			`{"date":"2026-13-01","distanceM":5000,"timeSecs":1350}`, // 日期非法
			`{"date":"2026-05-01","distanceM":0,"timeSecs":1350}`,    // 距离为 0
			`{"date":"2026-05-01","distanceM":5000,"timeSecs":0}`,    // 用时为 0
			`{"date":"2026-05-01","distanceM":5000,"timeSecs":60}`,   // 配速 0:12/km 荒谬
		}
		for i, body := range cases {
			w := do(h, http.MethodPost, "/api/athletes/1/race-results", token, body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("case %d 期望 400, 得到 %d: %s", i, w.Code, w.Body.String())
			}
		}
	})

	t.Run("更新与删除", func(t *testing.T) {
		path := fmt.Sprintf("/api/athletes/1/race-results/%d", created.ID)
		w := do(h, http.MethodPut, path, token,
			`{"date":"2026-05-01","distanceM":5000,"timeSecs":1320,"name":"春季 5K 修正"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d: %s", w.Code, w.Body.String())
		}
		w = do(h, http.MethodDelete, path, token, "")
		if w.Code != http.StatusNoContent {
			t.Fatalf("期望 204, 得到 %d", w.Code)
		}
		// 再删一次 → 404
		w = do(h, http.MethodDelete, path, token, "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("重复删除期望 404, 得到 %d", w.Code)
		}
	})
}
