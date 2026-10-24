package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/openrunner/server/store"
)

func TestHealthEndpoint(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()

	mux := NewMux(db, "dev")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200, 得到 %d", rec.Code)
	}

	var body struct {
		Status   string `json:"status"`
		Version  string `json:"version"`
		Database string `json:"database"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
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
