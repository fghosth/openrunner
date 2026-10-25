package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/openrunner/core/garmin"
	"github.com/openrunner/server/store"
	"gorm.io/gorm"
)

// platformDTO 平台连接（凭证永不出 API）。
type platformDTO struct {
	ID            uint   `json:"id"`
	Platform      string `json:"platform"`
	SyncCursor    string `json:"syncCursor"`
	LastSyncAt    int64  `json:"lastSyncAt"`
	LastError     string `json:"lastError"`
	Enabled       bool   `json:"enabled"`
	HasCredential bool   `json:"hasCredential"`
}

func toPlatformDTO(c *store.PlatformConnectionModel) platformDTO {
	return platformDTO{
		ID: c.ID, Platform: c.Platform, SyncCursor: c.SyncCursor,
		LastSyncAt: c.LastSyncAt, LastError: c.LastError, Enabled: c.Enabled,
		HasCredential: len(c.CredentialEnc) > 0,
	}
}

// supportedPlatforms 已实现的平台（票 15：Garmin 为首个）。
var supportedPlatforms = map[string]bool{"garmin": true}

// handlePlatformUpsert 保存/更新平台凭证（加密存储）。
func handlePlatformUpsert(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		platform := r.PathValue("platform")
		if !supportedPlatforms[platform] {
			writeError(w, http.StatusBadRequest, "不支持的平台（当前支持: garmin）")
			return
		}
		var body struct {
			APIKey  string `json:"apiKey"`
			Enabled *bool  `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "非法请求体")
			return
		}
		if strings.TrimSpace(body.APIKey) == "" {
			writeError(w, http.StatusBadRequest, "缺少 apiKey")
			return
		}

		root, err := store.JWTSecret(db)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取应用密钥失败")
			return
		}
		enc, err := store.EncryptCredential(root, []byte(strings.TrimSpace(body.APIKey)))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "加密凭证失败")
			return
		}
		conn := store.PlatformConnectionModel{
			AthleteID: uint(athleteID), Platform: platform,
			CredentialEnc: enc, Enabled: true,
		}
		if body.Enabled != nil {
			conn.Enabled = *body.Enabled
		}
		if err := store.SavePlatformConnection(db, &conn); err != nil {
			var ve *store.ValidationError
			if asValidationError(err, &ve) {
				writeError(w, http.StatusBadRequest, ve.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "保存失败")
			return
		}
		saved, err := store.GetPlatformConnection(db, uint(athleteID), platform)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		writeJSON(w, http.StatusCreated, toPlatformDTO(saved))
	}
}

// handlePlatformList 列出平台连接。
func handlePlatformList(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		conns, err := store.ListPlatformConnections(db, uint(athleteID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		out := make([]platformDTO, 0, len(conns))
		for i := range conns {
			out = append(out, toPlatformDTO(&conns[i]))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// handlePlatformDelete 删除平台连接（撤销凭证）。
func handlePlatformDelete(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err1 := pathValueID(r, "athleteId")
		platform := r.PathValue("platform")
		if err1 != nil || platform == "" {
			writeError(w, http.StatusBadRequest, "非法参数")
			return
		}
		if err := store.DeletePlatformConnection(db, uint(athleteID), platform); err != nil {
			if errors.Is(err, store.ErrPlatformNotFound) {
				writeError(w, http.StatusNotFound, "平台连接不存在")
				return
			}
			writeError(w, http.StatusInternalServerError, "删除失败")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// syncResult 同步结果（票 15：状态反馈）。
type syncResult struct {
	Imported int    `json:"imported"`
	Skipped  int    `json:"skipped"`
	Failed   int    `json:"failed"`
	Status   string `json:"status"` // ok / error
	Message  string `json:"message"`
	Cursor   string `json:"cursor"`
}

// handlePlatformSync 手动触发同步（票 15）。逻辑与 cron 共用 runPlatformSync。
func handlePlatformSync(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		platform := r.PathValue("platform")
		if !supportedPlatforms[platform] {
			writeError(w, http.StatusBadRequest, "不支持的平台")
			return
		}
		res := runPlatformSync(db, uint(athleteID), platform)
		code := http.StatusOK
		if res.Status == "error" {
			code = http.StatusBadGateway
		}
		writeJSON(w, code, res)
	}
}

// TriggerPlatformSync 供 cron 调用的同步入口（与 HTTP 端点共用实现）。
func TriggerPlatformSync(db *gorm.DB, athleteID uint, platform string) syncResult {
	return runPlatformSync(db, athleteID, platform)
}

// runPlatformSync 执行一次同步（Garmin 实现）。手动与 cron 共用。
//
// 流程：解密凭证 → 换 token → 拉增量列表（since=游标或 30 天前）→
// 逐个下载 ZIP→FIT → 复用 importOne 解析入库（content_hash 去重）→ 更新游标。
func runPlatformSync(db *gorm.DB, athleteID uint, platform string) syncResult {
	run := store.SyncRunModel{
		AthleteID: athleteID, Platform: platform,
		StartedAt: time.Now().Unix(), Status: "running",
	}
	_ = db.Create(&run).Error
	finish := func(res syncResult) syncResult {
		run.FinishedAt = time.Now().Unix()
		run.Imported, run.Skipped, run.Failed = res.Imported, res.Skipped, res.Failed
		run.Status, run.Message = res.Status, res.Message
		_ = db.Save(&run).Error
		return res
	}

	conn, err := store.GetPlatformConnection(db, athleteID, platform)
	if errors.Is(err, store.ErrPlatformNotFound) {
		return finish(syncResult{Status: "error", Message: "未配置该平台"})
	}
	if err != nil {
		return finish(syncResult{Status: "error", Message: "查询平台连接失败"})
	}
	if !conn.Enabled {
		return finish(syncResult{Status: "error", Message: "该平台连接已停用"})
	}

	root, err := store.JWTSecret(db)
	if err != nil {
		return finish(syncResult{Status: "error", Message: "读取应用密钥失败"})
	}
	apiKey, err := store.DecryptCredential(root, conn.CredentialEnc)
	if err != nil {
		return finish(syncResult{Status: "error", Message: "凭证解密失败（密钥是否已更换？）"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// 增量起点：游标（含当天重拉一次，靠 content_hash 去重）或默认 30 天
	since := time.Now().AddDate(0, 0, -30)
	if conn.SyncCursor != "" {
		if t, err := time.Parse("2006-01-02", conn.SyncCursor); err == nil {
			since = t
		}
	}

	res := syncOnePlatform(ctx, db, athleteID, platform, string(apiKey), since)
	if res.Status == "ok" {
		_ = store.UpdateSyncState(db, conn.ID, time.Now().Format("2006-01-02"), time.Now().Unix(), "")
	} else {
		_ = store.UpdateSyncState(db, conn.ID, conn.SyncCursor, time.Now().Unix(), res.Message)
	}
	return finish(res)
}

// garminBaseURL 可被测试覆盖（S2 fixture 回放：指向 httptest 服务）。
var garminBaseURL = ""

// syncOnePlatform 按平台分派（当前仅 Garmin）。
func syncOnePlatform(ctx context.Context, db *gorm.DB, athleteID uint, platform, apiKey string, since time.Time) syncResult {
	return syncGarmin(ctx, db, athleteID, apiKey, since)
}

// syncGarmin Garmin 同步实现（票 15）。
func syncGarmin(ctx context.Context, db *gorm.DB, athleteID uint, apiKey string, since time.Time) syncResult {
	c := garmin.NewClient(garminBaseURL, apiKey)
	token, err := c.ExchangeToken(ctx)
	if err != nil {
		msg := "换取 token 失败"
		if garmin.IsAuthError(err) {
			msg = "API Key 无效或已过期，请在 Garmin Connect 重新生成"
		}
		return syncResult{Status: "error", Message: msg}
	}
	list, err := c.ListActivitiesSince(ctx, token, since)
	if err != nil {
		msg := "拉取活动列表失败"
		if garmin.IsAuthError(err) {
			msg = "access token 失效"
		}
		return syncResult{Status: "error", Message: msg}
	}

	res := syncResult{Status: "ok"}
	for _, a := range list {
		if a.ActivityID <= 0 {
			continue
		}
		// 幂等：content_hash 已在 importOne 内处理；此处失败只计数不中断
		fit, err := c.DownloadActivityFIT(ctx, token, a.ActivityID)
		if err != nil {
			res.Failed++
			continue
		}
		out := importOne(db, athleteID, "garmin-"+itoa(a.ActivityID)+".fit", fit)
		switch out.Status {
		case "imported":
			res.Imported++
		case "skipped":
			res.Skipped++
		default:
			res.Failed++
		}
	}
	res.Cursor = time.Now().Format("2006-01-02")
	res.Message = "同步完成"
	return res
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	neg := v < 0
	if neg {
		v = -v
	}
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
