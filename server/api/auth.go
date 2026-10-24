package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/openrunner/server/store"
	"gorm.io/gorm"
)

// handleAuthStatus 报告首次启动状态（公开端点）。
func handleAuthStatus(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		needsSetup, err := store.NeedsSetup(db)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"needsSetup": needsSetup})
	}
}

// handleAuthSetup 创建管理员账号（仅首次；重复返回 409）。
func handleAuthSetup(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "非法请求体")
			return
		}
		if err := store.CreateUser(db, body.Username, body.Password); err != nil {
			var ve *store.ValidationError
			switch {
			case errors.Is(err, store.ErrUserExists):
				writeError(w, http.StatusConflict, "管理员已初始化")
			case errors.As(err, &ve):
				writeError(w, http.StatusBadRequest, ve.Error())
			default:
				writeError(w, http.StatusInternalServerError, "创建失败")
			}
			return
		}
		token, err := store.IssueToken(db, body.Username)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "签发 token 失败")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"token": token})
	}
}

// handleAuthLogin 校验账密并签发 token（公开端点）。
func handleAuthLogin(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "非法请求体")
			return
		}
		if err := store.Authenticate(db, body.Username, body.Password); err != nil {
			writeError(w, http.StatusUnauthorized, "账密错误")
			return
		}
		token, err := store.IssueToken(db, body.Username)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "签发 token 失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"token": token})
	}
}

// requireAuth 包裹需要登录的业务端点：校验 Bearer JWT。
func requireAuth(db *gorm.DB, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(auth, "Bearer ")
		if !ok || token == "" {
			writeError(w, http.StatusUnauthorized, "缺少凭证")
			return
		}
		if _, err := store.ParseToken(db, token); err != nil {
			writeError(w, http.StatusUnauthorized, "凭证无效或已过期")
			return
		}
		next(w, r)
	}
}
