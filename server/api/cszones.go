package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/openrunner/core"
	"github.com/openrunner/server/store"
	"gorm.io/gorm"
)

// handleCsZoneUpsert 手动设置 CS/D'。
func handleCsZoneUpsert(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		var body struct {
			ID        uint    `json:"id"`
			ValidFrom string  `json:"validFrom"`
			ValidTo   string  `json:"validTo"`
			Cs        float64 `json:"cs"`
			Dprime    float64 `json:"dprime"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "非法请求体")
			return
		}
		z := store.CsZoneModel{
			ID: body.ID, AthleteID: uint(athleteID),
			ValidFrom: body.ValidFrom, ValidTo: body.ValidTo,
			Cs: body.Cs, Dprime: body.Dprime, Source: "manual",
		}
		if err := store.UpsertCsZone(db, &z); err != nil {
			var ve *store.ValidationError
			if asValidationError(err, &ve) {
				writeError(w, http.StatusBadRequest, ve.Error())
				return
			}
			if errors.Is(err, gorm.ErrRecordNotFound) {
				writeError(w, http.StatusNotFound, "CS 区间不存在")
				return
			}
			writeError(w, http.StatusInternalServerError, "保存失败")
			return
		}
		writeJSON(w, http.StatusCreated, z)
	}
}

// handleCsZoneList 列出 CS 区间。
func handleCsZoneList(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		zones, err := store.ListCsZones(db, uint(athleteID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		writeJSON(w, http.StatusOK, zones)
	}
}

// handleFitCS 从跑步活动的速度流自动拟合 CS/D'（票 08）。
// 全力段时长：3/5/7/10/12/15 分钟（GC deriveCPParameters 挑点口径的跑步版）。
var fitDurations = []int{180, 300, 420, 600, 720, 900}

func handleFitCS(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		// 收集档案内全部跑步活动的速度流 → 全力段点
		var acts []store.ActivityModel
		if err := db.Where("athlete_id = ? AND sport = ? AND has_stream = ?", uint(athleteID), string(core.SportRun), true).
			Find(&acts).Error; err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		if len(acts) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"fitted": false, "reason": "没有带速度流的跑步活动"})
			return
		}

		best := map[int]float64{} // duration → max distance
		for _, a := range acts {
			sm, err := store.GetStream(db, a.ID)
			if err != nil {
				continue
			}
			stream, err := core.UnmarshalStream(sm.Data)
			if err != nil || stream.RecIntervalSecs <= 0 {
				continue
			}
			for _, pt := range core.BestEfforts(stream.SpeedKph, stream.RecIntervalSecs, fitDurations) {
				if pt.DistanceM > best[int(pt.Seconds)] {
					best[int(pt.Seconds)] = pt.DistanceM
				}
			}
		}
		pts := make([]core.EffortPoint, 0, len(best))
		for _, d := range fitDurations {
			if dist, ok := best[d]; ok && dist > 0 {
				pts = append(pts, core.EffortPoint{Seconds: float64(d), DistanceM: dist})
			}
		}
		if len(pts) < 2 {
			writeJSON(w, http.StatusOK, map[string]any{"fitted": false, "reason": "全力段点不足（需 ≥2 个时长）"})
			return
		}

		cs, dprime, quality := core.FitCriticalSpeed(pts)
		if quality.R2 < 0.9 || cs <= 0 || dprime <= 0 {
			writeJSON(w, http.StatusOK, map[string]any{
				"fitted": false, "reason": "拟合质量不足（R²<0.9 或参数非正）", "r2": quality.R2,
			})
			return
		}

		// 写入从最早活动日期起生效的 fit 区间
		from := timeToDateString(acts[0].StartTime)
		z := store.CsZoneModel{
			AthleteID: uint(athleteID), ValidFrom: from, ValidTo: "2099-12-31",
			Cs: cs, Dprime: dprime, Source: "fit",
		}
		if err := store.UpsertCsZone(db, &z); err != nil {
			writeError(w, http.StatusInternalServerError, "保存失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"fitted": true, "cs": cs, "dprime": dprime, "r2": quality.R2, "rmseM": quality.RMSEM, "n": quality.N,
		})
	}
}

// handleActivityDBal 活动的 D'bal 序列（有速度流 + 活动日期 CS 配置时）。
func handleActivityDBal(db *gorm.DB) http.HandlerFunc {
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
		sm, err := store.GetStream(db, m.ID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusNotFound, "该活动无 Stream")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		zone, err := store.CsZoneForDate(db, uint(athleteID), timeToDateString(m.StartTime))
		if errors.Is(err, store.ErrCsZoneNotFound) {
			writeError(w, http.StatusNotFound, "未配置 CS/D'（先手动设置或拟合）")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		stream, err := core.UnmarshalStream(sm.Data)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Stream 解析失败")
			return
		}
		bal := core.DBal(stream.SpeedKph, zone.Cs, zone.Dprime, stream.RecIntervalSecs)
		writeJSON(w, http.StatusOK, map[string]any{
			"cs": zone.Cs, "dprime": zone.Dprime, "dbal": bal, "intervalSecs": stream.RecIntervalSecs,
		})
	}
}
