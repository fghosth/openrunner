package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/openrunner/core"
	"github.com/openrunner/server/store"
	"gorm.io/gorm"
)

// handleAthleteList 列出全部 Athlete 档案。
func handleAthleteList(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		var models []store.AthleteModel
		if err := db.Order("id").Find(&models).Error; err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		writeJSON(w, http.StatusOK, models)
	}
}

// handleAthleteCreate 创建 Athlete（领域校验复用 openrunner-core）。
func handleAthleteCreate(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "非法请求体")
			return
		}
		athlete := core.Athlete{Name: body.Name}
		if err := athlete.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		m := store.AthleteModel{Name: athlete.Name}
		if err := db.Create(&m).Error; err != nil {
			writeError(w, http.StatusInternalServerError, "创建失败")
			return
		}
		writeJSON(w, http.StatusCreated, m)
	}
}

// handleAthleteUpdate 重命名 Athlete。
func handleAthleteUpdate(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法 id")
			return
		}
		var body struct {
			Name      string  `json:"name"`
			Sex       string  `json:"sex"`
			AvatarURL *string `json:"avatarUrl"`
			Bio       *string `json:"bio"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "非法请求体")
			return
		}
		if err := (core.Athlete{Name: body.Name}).Validate(); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		updates := map[string]any{"name": body.Name}
		// 指针字段：只有在请求体里显式出现时才更新（旧客户端不带这两个字段，
		// 不应把已有头像/简介清空）。
		if body.AvatarURL != nil {
			updates["avatar_url"] = *body.AvatarURL
		}
		if body.Bio != nil {
			updates["bio"] = *body.Bio
		}
		if body.Sex == "M" || body.Sex == "F" {
			updates["sex"] = body.Sex
		}
		res := db.Model(&store.AthleteModel{}).Where("id = ?", id).Updates(updates)
		if res.Error != nil {
			writeError(w, http.StatusInternalServerError, "更新失败")
			return
		}
		if res.RowsAffected == 0 {
			writeError(w, http.StatusNotFound, "Athlete 不存在")
			return
		}
		var m store.AthleteModel
		if err := db.First(&m, id).Error; err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		writeJSON(w, http.StatusOK, m)
	}
}

// handleAthleteDelete 删除 Athlete（活动数据的级联策略随后续票定）。
func handleAthleteDelete(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法 id")
			return
		}
		res := db.Delete(&store.AthleteModel{}, id)
		if res.Error != nil {
			writeError(w, http.StatusInternalServerError, "删除失败")
			return
		}
		if res.RowsAffected == 0 {
			writeError(w, http.StatusNotFound, "Athlete 不存在")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}
