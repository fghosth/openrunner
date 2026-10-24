package api

import (
	"encoding/json"
	"net/http"

	"github.com/openrunner/server/store"
	"gorm.io/gorm"
)

// handlePaceZoneUpsert 创建/更新阈值配速区间。
func handlePaceZoneUpsert(db *gorm.DB) http.HandlerFunc {
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
			ID                     uint   `json:"id"`
			ValidFrom              string `json:"validFrom"`
			ValidTo                string `json:"validTo"`
			ThresholdPaceSecsPerKm int    `json:"thresholdPaceSecsPerKm"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "非法请求体")
			return
		}
		z := store.PaceZoneModel{
			ID: body.ID, AthleteID: uint(athleteID),
			ValidFrom: body.ValidFrom, ValidTo: body.ValidTo,
			ThresholdPaceSecsPerKm: body.ThresholdPaceSecsPerKm,
		}
		if err := store.UpsertPaceZone(db, &z); err != nil {
			var ve *store.ValidationError
			if asValidationError(err, &ve) {
				writeError(w, http.StatusBadRequest, ve.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "保存失败")
			return
		}
		writeJSON(w, http.StatusCreated, z)
	}
}

// handlePaceZoneList 列出配速区间。
func handlePaceZoneList(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		zones, err := store.ListPaceZones(db, uint(athleteID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		writeJSON(w, http.StatusOK, zones)
	}
}
