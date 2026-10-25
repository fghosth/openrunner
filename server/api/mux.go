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

	mux.HandleFunc("POST /api/athletes/{athleteId}/activities", requireAuth(db, handleActivityCreate(db)))
	mux.HandleFunc("POST /api/athletes/{athleteId}/activities/import", requireAuth(db, handleActivityImport(db)))
	mux.HandleFunc("POST /api/athletes/{athleteId}/activities/import-batch", requireAuth(db, handleActivityImportBatch(db)))
	mux.HandleFunc("GET /api/athletes/{athleteId}/activities", requireAuth(db, handleActivityList(db)))
	mux.HandleFunc("GET /api/athletes/{athleteId}/activities/{activityId}", requireAuth(db, handleActivityDetail(db)))
	mux.HandleFunc("GET /api/athletes/{athleteId}/activities/{activityId}/stream", requireAuth(db, handleActivityStream(db)))
	mux.HandleFunc("POST /api/athletes/{athleteId}/activities/{activityId}/recompute", requireAuth(db, handleActivityRecompute(db)))

	mux.HandleFunc("GET /api/athletes/{athleteId}/hr-zones", requireAuth(db, handleHrZoneList(db)))
	mux.HandleFunc("PUT /api/athletes/{athleteId}/hr-zones", requireAuth(db, handleHrZoneUpsert(db)))
	mux.HandleFunc("GET /api/athletes/{athleteId}/pace-zones", requireAuth(db, handlePaceZoneList(db)))
	mux.HandleFunc("PUT /api/athletes/{athleteId}/pace-zones", requireAuth(db, handlePaceZoneUpsert(db)))

	mux.HandleFunc("GET /api/athletes/{athleteId}/seasons", requireAuth(db, handleSeasonList(db)))
	mux.HandleFunc("PUT /api/athletes/{athleteId}/seasons", requireAuth(db, handleSeasonUpsert(db)))
	mux.HandleFunc("DELETE /api/athletes/{athleteId}/seasons/{seasonId}", requireAuth(db, handleSeasonDelete(db)))
	mux.HandleFunc("GET /api/athletes/{athleteId}/pmc", requireAuth(db, handleAthletePMC(db)))
	mux.HandleFunc("GET /api/athletes/{athleteId}/activities/tracks", requireAuth(db, handleActivityTracks(db)))
	mux.HandleFunc("GET /api/athletes/{athleteId}/stats/annual", requireAuth(db, handleAnnualStats(db)))
	mux.HandleFunc("GET /api/athletes/{athleteId}/export", requireAuth(db, handleExport(db)))

	mux.HandleFunc("GET /api/athletes/{athleteId}/cs-zones", requireAuth(db, handleCsZoneList(db)))
	mux.HandleFunc("PUT /api/athletes/{athleteId}/cs-zones", requireAuth(db, handleCsZoneUpsert(db)))
	mux.HandleFunc("POST /api/athletes/{athleteId}/fit-cs", requireAuth(db, handleFitCS(db)))
	mux.HandleFunc("GET /api/athletes/{athleteId}/activities/{activityId}/dbal", requireAuth(db, handleActivityDBal(db)))
	mux.HandleFunc("GET /api/athletes/{athleteId}/activities/{activityId}/wbal", requireAuth(db, handleActivityWBal(db)))

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
