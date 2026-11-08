package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestCsZoneDelete(t *testing.T) {
	h, _ := newTestServer(t)
	token := setupAdmin(t, h)
	_ = createAthlete(t, h, token)
	w := do(h, http.MethodPut, "/api/athletes/1/cs-zones", token,
		`{"domain":"speed","validFrom":"2026-01-01","validTo":"2026-12-31","cs":3.4,"dprime":180,"source":"manual"}`)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("创建失败 %d %s", w.Code, w.Body.String())
	}
	var z map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &z)
	id := uint(z["id"].(float64))
	if w := do(h, http.MethodDelete, fmt.Sprintf("/api/athletes/1/cs-zones/%d", id), token, ""); w.Code != http.StatusNoContent {
		t.Fatalf("删除应 204，得到 %d", w.Code)
	}
	if w := do(h, http.MethodGet, "/api/athletes/1/cs-zones", token, ""); strings.Contains(w.Body.String(), `"3.4"`) || strings.Contains(w.Body.String(), "3.4") {
		t.Fatal("删除后列表仍含该记录")
	}
	if w := do(h, http.MethodDelete, fmt.Sprintf("/api/athletes/1/cs-zones/%d", id), token, ""); w.Code != http.StatusNotFound {
		t.Fatalf("重复删除应 404，得到 %d", w.Code)
	}
}
