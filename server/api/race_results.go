package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/openrunner/server/store"
	"gorm.io/gorm"
)

// handleRaceResultList 列出比赛成绩。
func handleRaceResultList(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		list, err := store.ListRaceResults(db, uint(athleteID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		writeJSON(w, http.StatusOK, list)
	}
}

// handleRaceResultCreate 新增比赛成绩。
func handleRaceResultCreate(db *gorm.DB) http.HandlerFunc {
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
			Date      string  `json:"date"`
			DistanceM float64 `json:"distanceM"`
			TimeSecs  int     `json:"timeSecs"`
			Name      string  `json:"name"`
			Note      string  `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "非法请求体")
			return
		}
		m := store.RaceResultModel{
			AthleteID: uint(athleteID), Date: body.Date, DistanceM: body.DistanceM,
			TimeSecs: body.TimeSecs, Name: body.Name, Note: body.Note,
		}
		if err := store.CreateRaceResult(db, &m); err != nil {
			var ve *store.ValidationError
			if asValidationError(err, &ve) {
				writeError(w, http.StatusBadRequest, ve.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "保存失败")
			return
		}
		writeJSON(w, http.StatusCreated, m)
	}
}

// handleRaceResultUpdate 更新比赛成绩。
func handleRaceResultUpdate(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		id, err := pathValueID(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法 id")
			return
		}
		var body struct {
			Date      string  `json:"date"`
			DistanceM float64 `json:"distanceM"`
			TimeSecs  int     `json:"timeSecs"`
			Name      string  `json:"name"`
			Note      string  `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "非法请求体")
			return
		}
		m := store.RaceResultModel{
			ID: uint(id), AthleteID: uint(athleteID), Date: body.Date,
			DistanceM: body.DistanceM, TimeSecs: body.TimeSecs, Name: body.Name, Note: body.Note,
		}
		if err := store.UpdateRaceResult(db, &m); err != nil {
			var ve *store.ValidationError
			if asValidationError(err, &ve) {
				writeError(w, http.StatusBadRequest, ve.Error())
				return
			}
			if errors.Is(err, store.ErrRaceResultNotFound) {
				writeError(w, http.StatusNotFound, "比赛成绩不存在")
				return
			}
			writeError(w, http.StatusInternalServerError, "更新失败")
			return
		}
		writeJSON(w, http.StatusOK, m)
	}
}

// handleRaceResultDelete 删除比赛成绩。
func handleRaceResultDelete(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		id, err := pathValueID(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法 id")
			return
		}
		if err := store.DeleteRaceResult(db, uint(athleteID), uint(id)); err != nil {
			if errors.Is(err, store.ErrRaceResultNotFound) {
				writeError(w, http.StatusNotFound, "比赛成绩不存在")
				return
			}
			writeError(w, http.StatusInternalServerError, "删除失败")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
