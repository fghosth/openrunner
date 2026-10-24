// Package api 定义 server 的 HTTP API（S3 黑盒接缝）。
package api

import (
	"encoding/json"
	"net/http"

	"gorm.io/gorm"
)

// NewMux 组装 API 全部路由（挂在任意前缀下均可）。
func NewMux(db *gorm.DB, version string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", Health(db, version))
	return mux
}

// Health 返回存活探针：服务版本与数据库连通性。
func Health(db *gorm.DB, version string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		database := "ok"
		if sqlDB, err := db.DB(); err != nil {
			database = "error"
		} else if err := sqlDB.Ping(); err != nil {
			database = "error"
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"status":   "ok",
			"version":  version,
			"database": database,
		})
	}
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
