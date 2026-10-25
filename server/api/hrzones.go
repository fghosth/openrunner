package api

import (
	"encoding/json"
	"net/http"

	"github.com/openrunner/server/store"
	"gorm.io/gorm"
)

// handleHrZoneUpsert 创建/更新心率区间。
func handleHrZoneUpsert(db *gorm.DB) http.HandlerFunc {
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
			ID        uint   `json:"id"`
			ValidFrom string `json:"validFrom"`
			ValidTo   string `json:"validTo"`
			MaxHr     int    `json:"maxHr"`
			RestHr    int    `json:"restHr"`
			LtHr      int    `json:"ltHr"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "非法请求体")
			return
		}
		z := store.HrZoneModel{
			ID: body.ID, AthleteID: uint(athleteID),
			ValidFrom: body.ValidFrom, ValidTo: body.ValidTo,
			MaxHr: body.MaxHr, RestHr: body.RestHr, LtHr: body.LtHr,
		}
		if err := store.UpsertHrZone(db, &z); err != nil {
			var ve *store.ValidationError
			if ok := asValidationError(err, &ve); ok {
				writeError(w, http.StatusBadRequest, ve.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "保存失败")
			return
		}
		writeJSON(w, http.StatusCreated, z)
	}
}

// handleHrZoneList 列出心率区间。
func handleHrZoneList(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		zones, err := store.ListHrZones(db, uint(athleteID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		writeJSON(w, http.StatusOK, zones)
	}
}

func asValidationError(err error, target **store.ValidationError) bool {
	ve, ok := err.(*store.ValidationError)
	if ok {
		*target = ve
	}
	return ok
}
