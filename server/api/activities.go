package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/openrunner/core"
	"github.com/openrunner/server/store"
	"gorm.io/gorm"
)

// maxUploadBytes 单文件上传上限（票 04 批量时按文件数放大）。
const maxUploadBytes = 64 << 20

// activityDTO 是活动的 API 契约（小驼峰，领域类型不直接出 API）。
type activityDTO struct {
	ID             uint    `json:"id"`
	AthleteID      uint    `json:"athleteId"`
	Sport          string  `json:"sport"`
	StartTime      int64   `json:"startTime"` // unix 秒（绝对时刻，展示层按本地时区渲染）
	DurationSecs   int     `json:"durationSecs"`
	TimerSecs      int     `json:"timerSecs"` // 不含停表段的时间（TRIMP 等负荷指标优先用它，票 05）
	DistanceM      float64 `json:"distanceM"`
	ElevationGainM float64 `json:"elevationGainM"`
	IsPlanned      bool    `json:"isPlanned"`
	HasStream      bool    `json:"hasStream"`
	HasGps         bool    `json:"hasGps"` // 有可用 GPS 轨迹（区别于「有 stream」）
	Source         string  `json:"source"`
}

func toDTO(m *store.ActivityModel) activityDTO {
	return activityDTO{
		ID: m.ID, AthleteID: m.AthleteID, Sport: m.Sport,
		StartTime: m.StartTime, DurationSecs: m.DurationSecs, TimerSecs: m.TimerSecs,
		DistanceM: m.DistanceM, ElevationGainM: m.ElevationGainM,
		IsPlanned: m.IsPlanned, HasStream: m.HasStream, HasGps: m.HasGps, Source: m.Source,
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
		if ok, err := store.AthleteExists(db, uint(athleteID)); err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		} else if !ok {
			writeError(w, http.StatusNotFound, "Athlete 不存在")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "multipart 解析失败（文件超限或格式错误）")
			return
		}
		defer func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll() // 清理额外 part 的临时文件
			}
		}()
		file, _, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "缺少 file 字段")
			return
		}
		defer file.Close()

		content, err := io.ReadAll(io.LimitReader(file, maxUploadBytes))
		if err != nil {
			writeError(w, http.StatusBadRequest, "读取文件失败")
			return
		}
		sum := sha256.Sum256(content)
		hash := hex.EncodeToString(sum[:])

		dup, err := store.FindActivityByHash(db, uint(athleteID), hash)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查重失败")
			return
		}
		if dup != nil {
			writeError(w, http.StatusConflict, "该文件已导入")
			return
		}

		activity, stream, source, err := parseActivityFile(content)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		id, err := store.CreateActivityWithStream(db, uint(athleteID), activity, stream, source, hash)
		if errors.Is(err, store.ErrSportNotCollected) {
			// 只收录跑步与骑行：返回 422 让调用方知道是「文件类型不支持」而不是「文件坏了」
			writeError(w, http.StatusUnprocessableEntity,
				"只收录跑步与骑行（该文件为 "+string(activity.Sport)+"）")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "保存失败")
			return
		}
		m, err := store.GetActivity(db, uint(athleteID), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		// 心率链指标尽力而为：失败可经 recompute 端点补算（区间/性别后配）
		if athlete, aerr := store.GetAthlete(db, uint(athleteID)); aerr == nil {
			_ = computeMetrics(db, athlete, m, &stream)
		}
		writeJSON(w, http.StatusCreated, toDTO(m))
	}
}

// handleActivityCreate 手工创建（计划/summary-only）活动，走估算路径（票 07）。
func handleActivityCreate(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		if ok, err := store.AthleteExists(db, uint(athleteID)); err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		} else if !ok {
			writeError(w, http.StatusNotFound, "Athlete 不存在")
			return
		}
		var body struct {
			Date         string  `json:"date"`
			Sport        string  `json:"sport"`
			DurationSecs int     `json:"durationSecs"`
			DistanceM    float64 `json:"distanceM"`
			AvgHr        float64 `json:"avgHr"`
			IsPlanned    bool    `json:"isPlanned"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "非法请求体")
			return
		}
		start, err := time.ParseInLocation("2006-01-02", body.Date, time.Local)
		if err != nil {
			writeError(w, http.StatusBadRequest, "日期无效（需 YYYY-MM-DD）")
			return
		}
		if body.DurationSecs <= 0 {
			writeError(w, http.StatusBadRequest, "时长必须为正")
			return
		}
		act := core.Activity{
			Sport:     core.Sport(body.Sport),
			StartTime: start,
			Duration:  time.Duration(body.DurationSecs) * time.Second,
			Distance:  body.DistanceM,
			IsPlanned: body.IsPlanned,
			AvgHr:     body.AvgHr,
		}
		if err := act.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		id, err := store.CreateActivityWithStream(db, uint(athleteID), act, core.Stream{}, "manual", "")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "保存失败")
			return
		}
		m, err := store.GetActivity(db, uint(athleteID), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		if athlete, aerr := store.GetAthlete(db, uint(athleteID)); aerr == nil {
			_ = computeMetrics(db, athlete, m, nil) // 估算路径
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
		if ok, err := store.AthleteExists(db, uint(athleteID)); err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		} else if !ok {
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
		metrics, err := getMetricsDTO(db, m.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		writeJSON(w, http.StatusOK, struct {
			activityDTO
			Metrics *metricsDTO `json:"metrics"`
		}{activityDTO: toDTO(m), Metrics: metrics})
	}
}

// handleActivityRecompute 重算活动的全部度量（心率链+配速链；区间/性别后配后的补算入口）。
func handleActivityRecompute(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err1 := pathValueID(r, "athleteId")
		activityID, err2 := pathValueID(r, "activityId")
		if err1 != nil || err2 != nil {
			writeError(w, http.StatusBadRequest, "非法 id")
			return
		}
		athlete, err := store.GetAthlete(db, uint(athleteID))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusNotFound, "Athlete 不存在")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		activity, err := store.GetActivity(db, uint(athleteID), uint(activityID))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusNotFound, "活动不存在")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		if err := recomputeMetrics(db, athlete, activity); err != nil {
			writeError(w, http.StatusInternalServerError, "重算失败")
			return
		}
		metrics, err := getMetricsDTO(db, activity.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		writeJSON(w, http.StatusOK, metrics)
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
		if _, err := store.GetActivity(db, uint(athleteID), uint(activityID)); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				writeError(w, http.StatusNotFound, "活动不存在")
			} else {
				writeError(w, http.StatusInternalServerError, "查询失败")
			}
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

// timeToDateString unix 秒 → YYYY-MM-DD（按服务器本地时区——自托管单用户即用户时区；
// 规格语义"跨午夜归开始日本地日期"，Round 5 决议解决挂账 #3）。
func timeToDateString(unix int64) string {
	return time.Unix(unix, 0).Local().Format("2006-01-02")
}
