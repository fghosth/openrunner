package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/fghosth/openrunner/core"
	"github.com/fghosth/openrunner/core/garmin"
	coreplatform "github.com/fghosth/openrunner/core/platform"
	_ "github.com/fghosth/openrunner/core/platform/coros"
	_ "github.com/fghosth/openrunner/core/platform/garmin"
	_ "github.com/fghosth/openrunner/core/platform/intervals"
	_ "github.com/fghosth/openrunner/core/platform/strava"
	"github.com/fghosth/openrunner/server/store"
	"gorm.io/gorm"
)

// platformDTO 平台连接（凭证永不出 API）。
type platformDTO struct {
	ID             uint   `json:"id"`
	Platform       string `json:"platform"`
	Region         string `json:"region"`
	CredentialKind string `json:"credentialKind"`
	SyncCursor     string `json:"syncCursor"`
	LastSyncAt     int64  `json:"lastSyncAt"`
	LastError      string `json:"lastError"`
	Enabled        bool   `json:"enabled"`
	HasCredential  bool   `json:"hasCredential"`
}

func toPlatformDTO(c *store.PlatformConnectionModel) platformDTO {
	return platformDTO{
		ID: c.ID, Platform: c.Platform, Region: c.Region, CredentialKind: c.CredentialKind,
		SyncCursor: c.SyncCursor,
		LastSyncAt: c.LastSyncAt, LastError: c.LastError, Enabled: c.Enabled,
		HasCredential: len(c.CredentialEnc) > 0,
	}
}

// supportedPlatforms 是已实现的平台集合 —— 来自 core/platform 的注册表，
// 新增平台只需在该包下实现 Provider + init() 注册，这里自动可见。
func supportedPlatforms() map[string]bool {
	out := map[string]bool{}
	for _, n := range coreplatform.Names() {
		out[n] = true
	}
	return out
}

// handlePlatformUpsert 保存/更新平台凭证（加密存储）。
func handlePlatformUpsert(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		platform := r.PathValue("platform")
		if !supportedPlatforms()[platform] {
			writeError(w, http.StatusBadRequest, "不支持的平台（当前支持: garmin）")
			return
		}
		// 凭证字段随平台而异（Garmin 的站点/token、Intervals 的 athleteId/key、
		// Strava 的三件套、COROS 的账号密码）。统一收成 JSON 键值对存库，
		// 具体要哪些字段由各平台 Provider.Validate 校验。
		var raw map[string]any
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			writeError(w, http.StatusBadRequest, "非法请求体")
			return
		}
		cred := map[string]string{}
		for k, v := range raw {
			if str, ok := v.(string); ok && strings.TrimSpace(str) != "" {
				cred[k] = strings.TrimSpace(str)
			}
		}
		enabled := true
		if v, ok := raw["enabled"].(bool); ok {
			enabled = v
		}

		// 凭证类型推断（决定 region 默认值与兼容旧字段）
		kind := "api_key"
		if cred["oauth1Token"] != "" || cred["token"] != "" {
			kind = "oauth1"
			if cred["oauth1Token"] == "" {
				cred["oauth1Token"] = cred["token"]
			}
			if cred["oauth1Secret"] == "" {
				cred["oauth1Secret"] = cred["secret"]
			}
			delete(cred, "token")
			delete(cred, "secret")
		}
		cred["credentialKind"] = kind
		region := "global"
		if strings.EqualFold(cred["region"], "cn") || (kind == "oauth1" && platform == "garmin") {
			region = "cn"
		}
		cred["region"] = region

		// 用平台自己的 Validate 做即时反馈（缺字段、格式错在保存时就报）
		prov, ok := coreplatform.Get(platform)
		if !ok {
			writeError(w, http.StatusBadRequest, "未实现的平台")
			return
		}
		if err := prov.Validate(coreplatform.Credential(cred)); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		plaintextBytes, err := json.Marshal(cred)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "序列化凭证失败")
			return
		}
		plaintext := string(plaintextBytes)

		root, err := store.JWTSecret(db)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取应用密钥失败")
			return
		}
		enc, err := store.EncryptCredential(root, []byte(plaintext))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "加密凭证失败")
			return
		}
		conn := store.PlatformConnectionModel{
			AthleteID: uint(athleteID), Platform: platform,
			Region: region, CredentialKind: kind,
			CredentialEnc: enc, Enabled: true,
		}
		conn.Enabled = enabled
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
		if !supportedPlatforms()[platform] {
			writeError(w, http.StatusBadRequest, "不支持的平台")
			return
		}
		// full=1：忽略游标，从最早日期重新拉全部历史（用于首次回填/补历史）
		full := r.URL.Query().Get("full") == "1"
		res := runPlatformSync(db, uint(athleteID), platform, full)
		code := http.StatusOK
		if res.Status == "error" {
			code = http.StatusBadGateway
		}
		writeJSON(w, code, res)
	}
}

// TriggerPlatformSync 供 cron 调用的同步入口（与 HTTP 端点共用实现）。
func TriggerPlatformSync(db *gorm.DB, athleteID uint, platform string) syncResult {
	return runPlatformSync(db, athleteID, platform, false)
}

// runPlatformSync 执行一次同步（Garmin 实现）。手动与 cron 共用。
//
// 流程：解密凭证 → 换 token → 拉增量列表（since=游标或 30 天前）→
// 逐个下载 ZIP→FIT → 复用 importOne 解析入库（content_hash 去重）→ 更新游标。
func runPlatformSync(db *gorm.DB, athleteID uint, platform string, full bool) syncResult {
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
	credentialRaw, err := store.DecryptCredential(root, conn.CredentialEnc)
	if err != nil {
		return finish(syncResult{Status: "error", Message: "凭证解密失败（密钥是否已更换？）"})
	}
	credential := string(credentialRaw)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// 起点选择：
	//   - full=true（手动"全量同步"）→ 从最早日期拉全部历史
	//   - 首次同步（无游标）→ 同样从最早日期拉全部历史（否则新用户只能看到最近 30 天）
	//   - 已有游标 → 从游标日期起增量（含当天重拉一次，靠 content_hash 去重）
	since := fullSyncStart
	if !full && conn.SyncCursor != "" {
		if t, err := time.Parse("2006-01-02", conn.SyncCursor); err == nil {
			since = t
		}
	}

	res := syncOnePlatform(ctx, db, athleteID, conn, credential, since)
	if res.Status == "ok" {
		_ = store.UpdateSyncState(db, conn.ID, time.Now().Format("2006-01-02"), time.Now().Unix(), "")
	} else {
		_ = store.UpdateSyncState(db, conn.ID, conn.SyncCursor, time.Now().Unix(), res.Message)
	}
	return finish(res)
}

// fullSyncStart 是全量同步的起点。Garmin Connect 的活动最早约在 2007 年，
// 这里取 2010-01-01 作为"拉全部历史"的哨兵值。
var fullSyncStart = time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)

// garminBaseURL 可被测试覆盖（S2 fixture 回放：指向 httptest 服务）。
// 生产为空 —— 由 region 决定真实主机。
var garminBaseURL = ""

// parseCredential 解析库里的凭证，并兼容旧格式。
//
// 历史数据有两种形态（票 15 时代）：
//   - api_key 模式：库里存的就是纯 API Key 字符串
//   - oauth1  模式：{"token":"…","secret":"…"}
//
// 新格式统一为 {"apiKey":"…"} / {"oauth1Token":"…","oauth1Secret":"…"} + credentialKind。
// 这里做一次性归一，老用户的凭证无需重新录入。
func parseCredential(raw string) coreplatform.Credential {
	var cred coreplatform.Credential
	if err := json.Unmarshal([]byte(raw), &cred); err == nil && len(cred) > 0 {
		// 旧 oauth1 字段名 → 新字段名
		if cred["oauth1Token"] == "" && cred["token"] != "" {
			cred["oauth1Token"] = cred["token"]
		}
		if cred["oauth1Secret"] == "" && cred["secret"] != "" {
			cred["oauth1Secret"] = cred["secret"]
		}
		if cred["credentialKind"] == "" && cred["oauth1Token"] != "" {
			cred["credentialKind"] = "oauth1"
		}
		if cred["credentialKind"] == "" {
			cred["credentialKind"] = "api_key"
		}
		return cred
	}
	// 旧 api_key 模式：整串就是 key
	return coreplatform.Credential{"apiKey": raw, "credentialKind": "api_key"}
}

// syncOnePlatform 按平台分派：统一走 core/platform 的 Provider 接口。
//
// 平台无关的共性职责都在这里：
//   - 业务键去重（开始时间 + 运动类型，FIT 字节每次下载都不同，content_hash 拦不住）
//   - 只收录跑步与骑行
//   - 逐个下载原始文件 → 复用 importOne 解析入库
//
// 各平台只需实现「拉列表 + 下载原始文件」两件事。
func syncOnePlatform(ctx context.Context, db *gorm.DB, athleteID uint, conn *store.PlatformConnectionModel, credential string, since time.Time) syncResult {
	// 测试注入桥接：api 层的 garminBaseURL 变量转发给平台包的统一注入点
	if conn.Platform == "garmin" && garminBaseURL != "" {
		coreplatform.SetBaseURL("garmin", garminBaseURL)
	} else if conn.Platform == "garmin" {
		coreplatform.SetBaseURL("garmin", "")
	}

	prov, ok := coreplatform.Get(conn.Platform)
	if !ok {
		return syncResult{Status: "error", Message: "未实现的平台: " + conn.Platform}
	}

	cred := parseCredential(credential)
	if err := prov.Validate(cred); err != nil {
		return syncResult{Status: "error", Message: err.Error()}
	}

	list, err := prov.ListActivities(ctx, cred, since)
	if err != nil {
		if coreplatform.IsAuthError(err) {
			return syncResult{Status: "error", Message: err.Error()}
		}
		return syncResult{Status: "error", Message: "拉取活动列表失败: " + err.Error()}
	}

	res := syncResult{Status: "ok"}
	for _, a := range list {
		if !core.IsCollectedSport(core.Sport(a.Sport)) {
			res.Skipped++
			continue
		}
		// 业务键去重：同一活动重复下载的文件字节不同，content_hash 拦不住
		if !a.StartTime.IsZero() {
			if dup, derr := store.FindActivityByStartAndSport(
				db, athleteID, a.StartTime.Unix(), string(a.Sport),
			); derr == nil && dup != nil {
				res.Skipped++
				continue
			}
		}
		data, filename, err := prov.Download(ctx, cred, a)
		if err != nil {
			res.Failed++
			continue
		}
		out := importOne(db, athleteID, filename, data)
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

// syncGarmin Garmin 同步实现（票 15 + 国行扩展）。
//
// 两种认证分派：
//   - region=global + api_key：Personal API Key → 换 token → personal.connectapi.garmin.com
//   - region=cn + oauth1   ：OAuth1 token/secret → 换 24h OAuth2 → connectapi.garmin.cn
//     （Garmin 中国站 Personal API 未开通，走网页会话等价路径，见 DEVLOG 票 15）
func syncGarmin(ctx context.Context, db *gorm.DB, athleteID uint, conn *store.PlatformConnectionModel, credential string, since time.Time) syncResult {
	c := garmin.NewClientForRegion(conn.Region, "")
	if garminBaseURL != "" { // 测试注入：指向 httptest，不跳过 TLS 校验
		c = garmin.NewClient(garminBaseURL, "")
	}

	var token string
	switch conn.CredentialKind {
	case "oauth1":
		var cred struct {
			Token  string `json:"token"`
			Secret string `json:"secret"`
		}
		if err := json.Unmarshal([]byte(credential), &cred); err != nil {
			return syncResult{Status: "error", Message: "OAuth1 凭据格式错误（请重新运行辅助脚本）"}
		}
		tok, err := c.ExchangeOAuth2(ctx, cred.Token, cred.Secret)
		if err != nil {
			msg := "换取 OAuth2 token 失败"
			if garmin.IsAuthError(err) {
				msg = "OAuth1 凭据失效，请重新运行辅助脚本获取"
			}
			return syncResult{Status: "error", Message: msg}
		}
		token = tok.AccessToken
	default: // api_key（Personal API）
		personalURL := garminBaseURL // 测试可覆盖
		c = garmin.NewClient(personalURL, credential)
		tok, err := c.ExchangeToken(ctx)
		if err != nil {
			msg := "换取 token 失败"
			if garmin.IsAuthError(err) {
				msg = "API Key 无效或已过期，请在 Garmin Connect 重新生成"
			}
			return syncResult{Status: "error", Message: msg}
		}
		token = tok
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
		// 只收录跑步与骑行：列表里先按类型过滤，省下无关活动的 FIT 下载。
		if !core.IsCollectedSport(core.Sport(a.Sport())) {
			res.Skipped++
			continue
		}
		// 业务键去重：同一活动重复下载的 FIT 字节不同，content_hash 拦不住
		// （实测每次同步都重新 imported 3 条），这里用「开始时间 + 类型」兜住。
		if a.StartTimeLocal != "" {
			if t, perr := time.ParseInLocation("2006-01-02 15:04:05", a.StartTimeLocal, time.Local); perr == nil {
				if dup, derr := store.FindActivityByStartAndSport(
					db, athleteID, t.Unix(), string(core.Sport(a.Sport())),
				); derr == nil && dup != nil {
					res.Skipped++
					continue
				}
			}
		}
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

// cleanCredentialValue 清洗用户粘贴的凭证：容忍 "oauth1_token = xxx"、"Bearer xxx"、
// 引号、前后空白与换行等噪音（用户从脚本输出整行复制是常见操作）。
func cleanCredentialValue(v string) string {
	v = strings.TrimSpace(v)
	v = strings.Trim(v, `"'`+"`")
	// 去掉 "key = value" 形式的前缀（只保留 = 之后部分）
	if i := strings.LastIndex(v, "="); i >= 0 && i < len(v)-1 {
		v = strings.TrimSpace(v[i+1:])
		v = strings.Trim(v, `"'`+"`")
	}
	// 去掉可能的 Bearer 前缀
	v = strings.TrimPrefix(v, "Bearer ")
	v = strings.TrimPrefix(v, "Basic ")
	return strings.TrimSpace(v)
}
