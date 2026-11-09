package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/fghosth/openrunner/core"
	"github.com/fghosth/openrunner/server/store"
	"gorm.io/gorm"
)

// handleActivityWBal 活动的 W'bal 序列（有功率流 + 活动日期 CS 配置时）。
// 跑步功率域：CP 即 CS（速度域阈值）、W' 即 D'——复用 cs_zones 存储（票 08 决策）。
func handleActivityWBal(db *gorm.DB) http.HandlerFunc {
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
		stream, err := core.UnmarshalStream(sm.Data)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Stream 解析失败")
			return
		}
		hasPower := len(stream.Power) > 0
		for _, p := range stream.Power {
			if p > 0 {
				hasPower = true
				break
			}
		}
		if !hasPower {
			writeError(w, http.StatusNotFound, "该活动无功率数据")
			return
		}
		zone, err := store.CsZoneForDate(db, uint(athleteID), "power", timeToDateString(m.StartTime))
		if errors.Is(err, store.ErrCsZoneNotFound) {
			writeError(w, http.StatusNotFound, "未配置 CP/W'（先手动设置或拟合）")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}

		cp := zone.Cs         // 功率域：CP（复用 cs 存储）
		wprime := zone.Dprime // 功率域：W'（复用 dprime 存储）
		pbarBelow := core.AvgPowerBelowCP(stream.Power, cp)
		tau := core.TauFromData(cp, pbarBelow)
		// 单活动 τ 覆盖（票 09②）：?tau= 查询参数优先
		if q := r.URL.Query().Get("tau"); q != "" {
			if v, perr := strconv.ParseFloat(q, 64); perr == nil && v > 0 {
				tau = v
			}
		}
		bal := core.WBal(stream.Power, cp, wprime, tau, stream.RecIntervalSecs)
		writeJSON(w, http.StatusOK, map[string]any{
			"cp": cp, "wprime": wprime, "tau": tau, "wbal": bal, "intervalSecs": stream.RecIntervalSecs,
		})
	}
}
