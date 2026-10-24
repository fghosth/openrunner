package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAthleteCRUD(t *testing.T) {
	h, _ := newTestServer(t)
	token := setupAdmin(t, h)

	t.Run("创建 Athlete 返回 201", func(t *testing.T) {
		w := do(h, http.MethodPost, "/api/athletes", token, `{"name":"我-公路"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("期望 201, 得到 %d: %s", w.Code, w.Body.String())
		}
		var m struct {
			ID   uint   `json:"ID"`
			Name string `json:"Name"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		if m.ID == 0 || m.Name != "我-公路" {
			t.Fatalf("期望 ID>0 且名字正确, 得到 %+v", m)
		}
	})

	t.Run("空名字被 400 拒绝", func(t *testing.T) {
		w := do(h, http.MethodPost, "/api/athletes", token, `{"name":""}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("期望 400, 得到 %d", w.Code)
		}
	})

	t.Run("列表包含刚创建的档案", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d", w.Code)
		}
		var list []struct {
			ID   uint   `json:"ID"`
			Name string `json:"Name"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &list)
		if len(list) != 1 || list[0].Name != "我-公路" {
			t.Fatalf("期望 1 个档案 我-公路, 得到 %+v", list)
		}
	})

	t.Run("重命名后列表反映新名字", func(t *testing.T) {
		w := do(h, http.MethodPut, "/api/athletes/1", token, `{"name":"我-越野"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d: %s", w.Code, w.Body.String())
		}
		w = do(h, http.MethodGet, "/api/athletes", token, "")
		var list []struct {
			Name string `json:"Name"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &list)
		if len(list) != 1 || list[0].Name != "我-越野" {
			t.Fatalf("期望重命名为 我-越野, 得到 %+v", list)
		}
	})

	t.Run("重命名不存在的档案返回 404", func(t *testing.T) {
		w := do(h, http.MethodPut, "/api/athletes/999", token, `{"name":"x"}`)
		if w.Code != http.StatusNotFound {
			t.Fatalf("期望 404, 得到 %d", w.Code)
		}
	})

	t.Run("删除后列表为空", func(t *testing.T) {
		w := do(h, http.MethodDelete, "/api/athletes/1", token, "")
		if w.Code != http.StatusNoContent {
			t.Fatalf("期望 204, 得到 %d", w.Code)
		}
		w = do(h, http.MethodGet, "/api/athletes", token, "")
		var list []any
		_ = json.Unmarshal(w.Body.Bytes(), &list)
		if len(list) != 0 {
			t.Fatalf("期望空列表, 得到 %d 项", len(list))
		}
	})

	t.Run("删除不存在的档案返回 404", func(t *testing.T) {
		w := do(h, http.MethodDelete, "/api/athletes/999", token, "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("期望 404, 得到 %d", w.Code)
		}
	})
}
