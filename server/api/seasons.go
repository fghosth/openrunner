package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/fghosth/openrunner/server/store"
	"gorm.io/gorm"
)

// handleSeasonUpsert 创建/更新赛季。
func handleSeasonUpsert(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		var body struct {
			ID        uint    `json:"id"`
			Name      string  `json:"name"`
			StartDate string  `json:"startDate"`
			EndDate   string  `json:"endDate"`
			Seed      float64 `json:"seed"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "非法请求体")
			return
		}
		season := store.SeasonModel{
			ID: body.ID, AthleteID: uint(athleteID),
			Name: body.Name, StartDate: body.StartDate, EndDate: body.EndDate, Seed: body.Seed,
		}
		if err := store.UpsertSeason(db, &season); err != nil {
			var ve *store.ValidationError
			if asValidationError(err, &ve) {
				writeError(w, http.StatusBadRequest, ve.Error())
				return
			}
			if errors.Is(err, gorm.ErrRecordNotFound) {
				writeError(w, http.StatusNotFound, "赛季不存在")
				return
			}
			writeError(w, http.StatusInternalServerError, "保存失败")
			return
		}
		writeJSON(w, http.StatusCreated, season)
	}
}

// handleSeasonList 列出赛季。
func handleSeasonList(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		seasons, err := store.ListSeasons(db, uint(athleteID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		writeJSON(w, http.StatusOK, seasons)
	}
}

// handleSeasonDelete 删除赛季。
func handleSeasonDelete(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err1 := pathValueID(r, "athleteId")
		seasonID, err2 := pathValueID(r, "seasonId")
		if err1 != nil || err2 != nil {
			writeError(w, http.StatusBadRequest, "非法 id")
			return
		}
		if err := store.DeleteSeason(db, uint(athleteID), uint(seasonID)); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				writeError(w, http.StatusNotFound, "赛季不存在")
				return
			}
			writeError(w, http.StatusInternalServerError, "删除失败")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
