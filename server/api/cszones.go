package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/fghosth/openrunner/core"
	"github.com/fghosth/openrunner/server/store"
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
			Domain    string  `json:"domain"` // speed / power
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
			ID: body.ID, AthleteID: uint(athleteID), Domain: body.Domain,
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
		// 收集档案内全部跑步活动（speed 域）或骑行活动（power 域）的速度/功率流
		domain := r.URL.Query().Get("domain")
		if domain != "power" {
			domain = "speed"
		}
		var acts []store.ActivityModel
		if domain == "power" {
			// 功率域：优先骑行（功率计语义最标准）；骑行不足时退回**全部带功率流的活动**
			// —— 现代 Garmin 手表会给跑步记录功率，用户只有 1 条骑行时若不做这个退回，
			// 功率负荷就永远算不出来。
			if err := db.Where("athlete_id = ? AND sport = ? AND has_stream = ?",
				uint(athleteID), string(core.SportRide), true).
				Order("start_time ASC").Find(&acts).Error; err != nil {
				writeError(w, http.StatusInternalServerError, "查询失败")
				return
			}
			if len(acts) < 2 {
				if err := db.Where("athlete_id = ? AND has_stream = ?", uint(athleteID), true).
					Order("start_time ASC").Find(&acts).Error; err != nil {
					writeError(w, http.StatusInternalServerError, "查询失败")
					return
				}
			}
		} else {
			if err := db.Where("athlete_id = ? AND sport = ? AND has_stream = ?",
				uint(athleteID), string(core.SportRun), true).
				Order("start_time ASC").Find(&acts).Error; err != nil {
				writeError(w, http.StatusInternalServerError, "查询失败")
				return
			}
		}
		// 功率域只保留真正有功率流的活动（跑步/骑行都可能）
		if domain == "power" {
			filtered := make([]store.ActivityModel, 0, len(acts))
			for i := range acts {
				if sm, err := store.GetStream(db, acts[i].ID); err == nil {
					if st, err := core.UnmarshalStream(sm.Data); err == nil {
						for _, v := range st.Power {
							if v > 0 {
								filtered = append(filtered, acts[i])
								break
							}
						}
					}
				}
			}
			acts = filtered
		}
		if len(acts) == 0 {
			reason := "没有带速度流的跑步活动"
			if domain == "power" {
				reason = "没有带功率流的活动（需要功率计，或支持跑步功率的手表）"
			}
			writeJSON(w, http.StatusOK, map[string]any{"fitted": false, "reason": reason})
			return
		}

		// 逐活动收集全力段（每活动一份点集），再交给 SelectFitPoints 挑点。
		// 不能像以前那样「每个时长全局取最大」——那会让同一次匀速跑在 600/720/900s
		// 上都占位，拟合出 R²≈1 但毫无生理意义的参数（实测 D'=71m）。
		perActivity := make([]core.ActivityEfforts, 0, len(acts))
		for _, a := range acts {
			sm, err := store.GetStream(db, a.ID)
			if err != nil {
				continue
			}
			stream, err := core.UnmarshalStream(sm.Data)
			if err != nil || stream.RecIntervalSecs <= 0 {
				continue
			}
			series := stream.SpeedKph
			if domain == "power" {
				series = stream.Power
			}
			hasData := false
			for _, v := range series {
				if v > 0 {
					hasData = true
					break
				}
			}
			if !hasData {
				continue
			}
			perActivity = append(perActivity, core.ActivityEfforts{
				ActivityID: a.ID,
				Points:     core.BestEfforts(series, stream.RecIntervalSecs, fitDurations),
			})
		}

		pts, nActs := core.SelectFitPoints(perActivity, fitDurations)
		if len(pts) < 2 {
			writeJSON(w, http.StatusOK, map[string]any{
				"fitted": false,
				"reason": "全力段点不足（需 ≥2 个时长都有覆盖）",
			})
			return
		}
		if nActs < 3 {
			writeJSON(w, http.StatusOK, map[string]any{
				"fitted": false,
				"reason": "参与拟合的活动只有 " + strconv.Itoa(nActs) + " 次，至少需要 3 次不同强度的跑步" +
					"（含间歇跑或计时跑），否则拟合结果没有参考价值",
			})
			return
		}

		var cs, dprime float64
		var quality core.FitQuality
		if domain == "power" {
			cs, dprime, quality = core.FitCPWPrime(pts) // CP (W) / W' (J)
		} else {
			cs, dprime, quality = core.FitCriticalSpeed(pts) // CS (m/s) / D' (m)
		}
		// 三参数模型（CP3）：与 2 参数并列返回，用于展示 Peak（Pmax / 瞬时极限）。
		// 写库仍用 2 参数结果，保持 D'bal/W'bal 的既有语义。
		cp3 := core.FitCP3(pts)
		cp3Body := map[string]any{}
		if cp3.Quality.RMSEM >= 0 {
			cp3Body = map[string]any{
				"cs": cp3.CS, "dprime": cp3.DPrime, "tau": cp3.Tau,
				"peak": cp3.Peak, "peakReliable": cp3.PeakReliable,
				"r2": cp3.Quality.R2, "rmse": cp3.Quality.RMSEM,
			}
		}

		// D'(m) / W'(J) 的合理区间：跑步 D' 成人正常 150–300m（放宽到 50–800），
		// 骑行 W' 通常 10–30kJ。落在区间外说明拟合退化，宁可不给也不要给错。
		minD, maxD := 50.0, 800.0
		if domain == "power" {
			minD, maxD = 3000, 60000
		}
		if cs > 0 && (dprime < minD || dprime > maxD) {
			writeJSON(w, http.StatusOK, map[string]any{
				"fitted": false,
				"reason": "拟合结果不合理（储备 " + strconv.FormatFloat(dprime, 'f', 0, 64) +
					"，合理区间 " + strconv.FormatFloat(minD, 'f', 0, 64) + "–" +
					strconv.FormatFloat(maxD, 'f', 0, 64) + "），通常是因为数据里缺少真正的全力段",
				"r2": quality.R2, "cp3": cp3Body,
			})
			return
		}
		if quality.R2 < 0.9 || cs <= 0 || dprime <= 0 {
			reason := "拟合质量不足（R²<0.9 或参数非正）"
			if domain == "power" {
				reason = "拟合质量不足（R²=" + strconv.FormatFloat(quality.R2, 'f', 2, 64) +
					" < 0.90）—— 跑步功率本身噪声较大，建议手动填写 CP：" +
					"跑步的临界功率通常约为 3.5–4.5 W/kg（例：60kg 的人约 210–270 W）"
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"fitted": false, "reason": reason, "r2": quality.R2, "cp3": cp3Body,
			})
			return
		}

		// 写入从最早活动日期起生效的 fit 区间。
		// Round 7：先删同域旧 fit 行——否则旧区间（valid_from 更早）会按 DESC 排序永久遮蔽新拟合。
		if err := db.Where("athlete_id = ? AND domain = ? AND source = ?", uint(athleteID), domain, "fit").
			Delete(&store.CsZoneModel{}).Error; err != nil {
			writeError(w, http.StatusInternalServerError, "清理旧拟合失败")
			return
		}
		from := timeToDateString(acts[0].StartTime) // acts 已按 start_time ASC
		z := store.CsZoneModel{
			AthleteID: uint(athleteID), Domain: domain, ValidFrom: from, ValidTo: "2099-12-31",
			Cs: cs, Dprime: dprime, Source: "fit",
		}
		if err := store.UpsertCsZone(db, &z); err != nil {
			writeError(w, http.StatusInternalServerError, "保存失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"fitted": true, "domain": domain, "cs": cs, "dprime": dprime,
			"r2": quality.R2, "rmseM": quality.RMSEM, "n": quality.N,
			"activities": nActs, "cp3": cp3Body,
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
		zone, err := store.CsZoneForDate(db, uint(athleteID), "speed", timeToDateString(m.StartTime))
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

// handleCsZoneDelete 删除一条临界域记录。
// DELETE /api/athletes/{{athleteId}}/cs-zones/{{zoneId}}
func handleCsZoneDelete(db *gorm.DB) http.HandlerFunc {
	{
		return func(w http.ResponseWriter, r *http.Request) {
			{
				athleteID, err := pathValueID(r, "athleteId")
				if err != nil {
					{
						writeError(w, http.StatusBadRequest, "非法档案 id")
						return
					}
				}
				zoneID, err := pathValueID(r, "zoneId")
				if err != nil {
					{
						writeError(w, http.StatusBadRequest, "非法区间 id")
						return
					}
				}
				if err := store.DeleteCsZone(db, uint(athleteID), uint(zoneID)); err != nil {
					{
						if errors.Is(err, store.ErrZoneNotFound) {
							{
								writeError(w, http.StatusNotFound, "记录不存在")
								return
							}
						}
						writeError(w, http.StatusInternalServerError, "删除失败")
						return
					}
				}
				w.WriteHeader(http.StatusNoContent)
			}
		}
	}
}
