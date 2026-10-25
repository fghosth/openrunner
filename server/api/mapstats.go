package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/openrunner/core"
	"github.com/openrunner/server/store"
	"gorm.io/gorm"
)

// trackDTO 地图轨迹（票 11）：抽稀后的 [lat,lon] 点对。
type trackDTO struct {
	ID        uint         `json:"id"`
	Sport     string       `json:"sport"`
	StartTime int64        `json:"startTime"`
	DistanceM float64      `json:"distanceM"`
	Points    [][2]float64 `json:"points"`
}

// handleActivityTracks 返回档案内全部有 GPS 的活动轨迹（默认每活动最多 300 点）。
// 无 GPS 的活动不返回（前端地图只画有轨迹的）。
func handleActivityTracks(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		maxPoints := 300
		if q := r.URL.Query().Get("max"); q != "" {
			if v, err := strconv.Atoi(q); err == nil && v > 0 && v <= 2000 {
				maxPoints = v
			}
		}

		list, err := store.ListActivities(db, uint(athleteID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}

		// 批量取 stream（避免 N+1：1000 活动不该发 1000 次查询）
		ids := make([]uint, 0, len(list))
		for i := range list {
			if list[i].HasStream {
				ids = append(ids, list[i].ID)
			}
		}
		streams := map[uint][]byte{}
		if len(ids) > 0 {
			var sms []store.ActivityStreamModel
			if err := db.Where("activity_id IN ?", ids).Find(&sms).Error; err != nil {
				writeError(w, http.StatusInternalServerError, "查询 Stream 失败")
				return
			}
			for i := range sms {
				streams[sms[i].ActivityID] = sms[i].Data
			}
		}

		out := make([]trackDTO, 0, len(list))
		for i := range list {
			a := &list[i]
			raw, ok := streams[a.ID]
			if !ok {
				continue
			}
			stream, err := core.UnmarshalStream(raw)
			if err != nil {
				continue
			}
			pts := core.DownsampleTrack(stream.Latitude, stream.Longitude, maxPoints)
			if len(pts) < 2 {
				continue // 无有效 GPS
			}
			out = append(out, trackDTO{
				ID: a.ID, Sport: a.Sport, StartTime: a.StartTime,
				DistanceM: a.DistanceM, Points: pts,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// handleAnnualStats 年度统计（票 11）：年/月汇总 + 连续天数。
func handleAnnualStats(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		year := time.Now().Local().Year()
		if q := r.URL.Query().Get("year"); q != "" {
			v, err := strconv.Atoi(q)
			if err != nil || v < 1900 || v > 2200 {
				writeError(w, http.StatusBadRequest, "非法年份")
				return
			}
			year = v
		}
		days, err := loadDayActivities(db, uint(athleteID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "聚合失败")
			return
		}
		stat := core.AnnualStats(days, year)
		writeJSON(w, http.StatusOK, struct {
			core.AnnualStat
			Streaks core.StreakStat `json:"streaks"`
		}{AnnualStat: stat, Streaks: core.Streaks(days)})
	}
}

// loadDayActivities 按活动本地日期聚合日级摘要。
func loadDayActivities(db *gorm.DB, athleteID uint) ([]core.DayActivity, error) {
	var rows []struct {
		StartTime    int64
		DistanceM    float64
		DurationSecs int
	}
	// 只统计实际活动：计划活动不应计入"今年跑了多少/多少天"
	err := db.Table("activities").
		Select("start_time, distance_m, duration_secs").
		Where("athlete_id = ? AND is_planned = ?", athleteID, false).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	byDate := map[string]*core.DayActivity{}
	order := make([]string, 0, len(rows))
	for _, row := range rows {
		d := timeToDateString(row.StartTime)
		if a, ok := byDate[d]; ok {
			a.DistanceM += row.DistanceM
			a.DurationSecs += row.DurationSecs
			a.Count++
			continue
		}
		byDate[d] = &core.DayActivity{
			Date: d, DistanceM: row.DistanceM, DurationSecs: row.DurationSecs, Count: 1,
		}
		order = append(order, d)
	}
	out := make([]core.DayActivity, 0, len(order))
	for _, d := range order {
		out = append(out, *byDate[d])
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

var _ = errors.Is
