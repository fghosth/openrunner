package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/openrunner/core/ingest"
	"github.com/openrunner/server/store"
	"gorm.io/gorm"
)

// activityDTO 是活动的 API 契约（小驼峰，领域类型不直接出 API）。
type activityDTO struct {
	ID             uint    `json:"id"`
	AthleteID      uint    `json:"athleteId"`
	Sport          string  `json:"sport"`
	StartTime      int64   `json:"startTime"` // unix 秒
	DurationSecs   int     `json:"durationSecs"`
	DistanceM      float64 `json:"distanceM"`
	ElevationGainM float64 `json:"elevationGainM"`
	IsPlanned      bool    `json:"isPlanned"`
	HasStream      bool    `json:"hasStream"`
	Source         string  `json:"source"`
}

func toDTO(m *store.ActivityModel) activityDTO {
	return activityDTO{
		ID: m.ID, AthleteID: m.AthleteID, Sport: m.Sport,
		StartTime: m.StartTime, DurationSecs: m.DurationSecs,
		DistanceM: m.DistanceM, ElevationGainM: m.ElevationGainM,
		IsPlanned: m.IsPlanned, HasStream: m.HasStream, Source: m.Source,
	}
}

// handleActivityImport 处理 FIT 文件上传（multipart，字段名 file）。
func handleActivityImport(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		if ok, err := store.AthleteExists(db, uint(athleteID)); err != nil || !ok {
			writeError(w, http.StatusNotFound, "Athlete 不存在")
			return
		}

		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "multipart 解析失败")
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "缺少 file 字段")
			return
		}
		defer file.Close()

		content, err := io.ReadAll(file)
		if err != nil {
			writeError(w, http.StatusBadRequest, "读取文件失败")
			return
		}
		sum := sha256.Sum256(content)
		hash := hex.EncodeToString(sum[:])

		if dup, err := store.FindActivityByHash(db, uint(athleteID), hash); err == nil && dup != nil {
			writeError(w, http.StatusConflict, "该文件已导入")
			return
		}

		activity, stream, err := ingest.ParseFIT(bytes.NewReader(content))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		id, err := store.CreateActivityWithStream(db, uint(athleteID), activity, stream, "import_fit", hash)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "保存失败")
			return
		}
		m, err := store.GetActivity(db, uint(athleteID), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		writeJSON(w, http.StatusCreated, toDTO(m))
	}
}

// handleActivityList 列出档案下全部活动。
func handleActivityList(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		if ok, err := store.AthleteExists(db, uint(athleteID)); err != nil || !ok {
			writeError(w, http.StatusNotFound, "Athlete 不存在")
			return
		}
		list, err := store.ListActivities(db, uint(athleteID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		out := make([]activityDTO, 0, len(list))
		for i := range list {
			out = append(out, toDTO(&list[i]))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// handleActivityDetail 返回单个活动 summary。
func handleActivityDetail(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err1 := pathValueID(r, "athleteId")
		activityID, err2 := pathValueID(r, "activityId")
		if err1 != nil || err2 != nil {
			writeError(w, http.StatusBadRequest, "非法 id")
			return
		}
		m, err := store.GetActivity(db, uint(athleteID), uint(activityID))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusNotFound, "活动不存在")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		writeJSON(w, http.StatusOK, toDTO(m))
	}
}

// handleActivityStream 返回活动 Stream（供前端画曲线）。
func handleActivityStream(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err1 := pathValueID(r, "athleteId")
		activityID, err2 := pathValueID(r, "activityId")
		if err1 != nil || err2 != nil {
			writeError(w, http.StatusBadRequest, "非法 id")
			return
		}
		if _, err := store.GetActivity(db, uint(athleteID), uint(activityID)); errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusNotFound, "活动不存在")
			return
		}
		sm, err := store.GetStream(db, uint(activityID))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusNotFound, "该活动无 Stream")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(sm.Data)
	}
}

func pathValueID(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(r.PathValue(name), 10, 64)
}
