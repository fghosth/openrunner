// Package api 定义 server 的 HTTP API（S3 黑盒接缝）。
//
// NewMux 组装完整路由：/health、/api/auth/*（公开）、/api/*（需登录）、
// 以及可选的 SPA 回退（生产由 web.Handler 提供，测试传 nil）。
package api

import (
	"encoding/json"
	"net/http"

	"gorm.io/gorm"
)

func NewMux(db *gorm.DB, version string, spa http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", Health(db, version))
	mux.HandleFunc("GET /api/health", Health(db, version))

	mux.HandleFunc("GET /api/auth/status", handleAuthStatus(db))
	mux.HandleFunc("POST /api/auth/setup", handleAuthSetup(db))
	mux.HandleFunc("POST /api/auth/login", handleAuthLogin(db))

	mux.HandleFunc("GET /api/athletes", requireAuth(db, handleAthleteList(db)))
	mux.HandleFunc("POST /api/athletes", requireAuth(db, handleAthleteCreate(db)))
	mux.HandleFunc("PUT /api/athletes/{id}", requireAuth(db, handleAthleteUpdate(db)))
	mux.HandleFunc("DELETE /api/athletes/{id}", requireAuth(db, handleAthleteDelete(db)))

	if spa != nil {
		mux.Handle("/", spa)
	}
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

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
