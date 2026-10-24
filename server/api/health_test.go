package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	h, _ := newTestServer(t)

	w := do(h, http.MethodGet, "/health", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 得到 %d", w.Code)
	}

	var body struct {
		Status   string `json:"status"`
		Version  string `json:"version"`
		Database string `json:"database"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("期望 status=ok, 得到 %q", body.Status)
	}
	if body.Version == "" {
		t.Errorf("期望返回版本号")
	}
	if body.Database != "ok" {
		t.Errorf("期望 database=ok, 得到 %q", body.Database)
	}
}

func TestHealthEndpointViaAPIPrefix(t *testing.T) {
	h, _ := newTestServer(t)

	// 前缀挂载形态（main.go 里 /api 前缀 + StripPrefix）
	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", h))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 得到 %d", w.Code)
	}
}
