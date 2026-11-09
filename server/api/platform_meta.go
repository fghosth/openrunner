package api

import (
	"net/http"

	"github.com/fghosth/openrunner/core/platform"
	"gorm.io/gorm"
)

// 平台凭证表单的元信息。
//
// Label/Help 是中文原文（保底，旧客户端仍可用）；LabelKey/HelpKey 是 i18n key，
// 前端优先按 key 取本地文案、取不到才回落原文。
//
// 为什么要有 key：服务端不该把中文当**数据**发给英文界面。这是票 07 i18n 的遗留——
// 表单标签/帮助文案由服务端下发，英文界面只能显示中文。判定的 detail 走的是
// 「结构化参数 + 前端模板」两层输出（见 coach 的 Params/ParamsStr），这里同理：
// 服务端给稳定标识，文案归前端。

// platformOption 下拉选项。
type platformOption struct {
	Value    string `json:"value"`
	Label    string `json:"label"`
	LabelKey string `json:"labelKey,omitempty"`
}

// platformField 是凭证表单里的一个输入项。
type platformField struct {
	Key         string           `json:"key"`
	Label       string           `json:"label"`
	LabelKey    string           `json:"labelKey,omitempty"`
	Type        string           `json:"type"` // text | password | select
	Placeholder string           `json:"placeholder,omitempty"`
	Help        string           `json:"help,omitempty"`
	HelpKey     string           `json:"helpKey,omitempty"`
	Options     []platformOption `json:"options,omitempty"`
}

// platformMeta 是一个平台的元信息（供前端动态渲染凭证表单）。
type platformMeta struct {
	Name           string          `json:"name"`
	Label          string          `json:"label"`
	LabelKey       string          `json:"labelKey,omitempty"`
	Help           string          `json:"help"`
	HelpKey        string          `json:"helpKey,omitempty"`
	Fields         []platformField `json:"fields"`
	SetupURL       string          `json:"setupUrl,omitempty"`
	CredentialJSON bool            `json:"credentialJson"` // 凭证是否以 JSON 存储（新平台都是）
}

// platformMetas 是各平台的 UI 元信息。
//
// 放在 api 层而不是 platform 包：这些是**给人看的文案**，
// 平台包只负责协议（保持可移植、无 UI 耦合）。
func platformMetas() []platformMeta {
	return []platformMeta{
		{
			Name: "garmin", Label: "Garmin", LabelKey: "platform.garmin.label",
			SetupURL: "https://connect.garmin.com/modern/settings/account",
			Help:     "国际站用 Personal API Key；中国站（garmin.cn）用 OAuth1 token/secret（由 scripts/garmin-cn-token.py 生成）",
			HelpKey:  "platform.garmin.help",
			Fields: []platformField{
				{Key: "region", Label: "站点", LabelKey: "platform.garmin.field.region.label", Type: "select",
					Options: []platformOption{
						{Value: "global", Label: "国际站（API Key）", LabelKey: "platform.garmin.option.global"},
						{Value: "cn", Label: "中国站（OAuth1）", LabelKey: "platform.garmin.option.cn"},
					}},
				{Key: "apiKey", Label: "Personal API Key", LabelKey: "platform.garmin.field.apiKey.label",
					Type: "password", Placeholder: "国际站用",
					Help: "Garmin Connect → 设置 → 账户 里生成", HelpKey: "platform.garmin.field.apiKey.help"},
				{Key: "oauth1Token", Label: "OAuth1 token", LabelKey: "platform.garmin.field.oauth1Token.label",
					Type: "password", Placeholder: "中国站用"},
				{Key: "oauth1Secret", Label: "OAuth1 secret", LabelKey: "platform.garmin.field.oauth1Secret.label",
					Type: "password", Placeholder: "中国站用"},
			},
		},
		{
			Name: "intervals", Label: "Intervals.icu", LabelKey: "platform.intervals.label",
			SetupURL: "https://intervals.icu/settings",
			Help:     "所有平台里最省事的：只要一个 personal API key，无需注册应用",
			HelpKey:  "platform.intervals.help",
			Fields: []platformField{
				{Key: "athleteId", Label: "Athlete ID", LabelKey: "platform.intervals.field.athleteId.label",
					Type: "text", Placeholder: "i12345",
					Help: "intervals.icu 个人页 URL 里的 i 开头编号", HelpKey: "platform.intervals.field.athleteId.help"},
				{Key: "apiKey", Label: "API Key", LabelKey: "platform.intervals.field.apiKey.label",
					Type: "password",
					Help: "Settings → Developer → API Key", HelpKey: "platform.intervals.field.apiKey.help"},
			},
		},
		{
			Name: "strava", Label: "Strava", LabelKey: "platform.strava.label",
			SetupURL: "https://www.strava.com/settings/api",
			Help:     "需先在 Strava 自助创建应用拿 client_id / secret，再走一次授权拿 refresh_token（access token 6 小时过期，会自动刷新）",
			HelpKey:  "platform.strava.help",
			Fields: []platformField{
				{Key: "clientId", Label: "Client ID", LabelKey: "platform.strava.field.clientId.label", Type: "text"},
				{Key: "clientSecret", Label: "Client Secret", LabelKey: "platform.strava.field.clientSecret.label", Type: "password"},
				{Key: "refreshToken", Label: "Refresh Token", LabelKey: "platform.strava.field.refreshToken.label",
					Type: "password",
					Help: "首次授权后获得，长期有效", HelpKey: "platform.strava.field.refreshToken.help"},
			},
		},
		{
			Name: "coros", Label: "COROS（高驰）", LabelKey: "platform.coros.label",
			Help:    "用 COROS App 的账号密码登录（非官方接口，若失败请改用文件导入）",
			HelpKey: "platform.coros.help",
			Fields: []platformField{
				{Key: "account", Label: "账号", LabelKey: "platform.coros.field.account.label",
					Type: "text", Placeholder: "手机号或邮箱"},
				{Key: "password", Label: "密码", LabelKey: "platform.coros.field.password.label", Type: "password"},
			},
		},
	}
}

// handlePlatformMeta 返回已实现平台的元信息（前端据此动态渲染凭证表单）。
//
// GET /api/platforms
func handlePlatformMeta(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		implemented := map[string]bool{}
		for _, n := range platform.Names() {
			implemented[n] = true
		}
		out := make([]platformMeta, 0)
		for _, m := range platformMetas() {
			if !implemented[m.Name] {
				continue // 只有真正注册了 Provider 的平台才出现在 UI 里
			}
			m.CredentialJSON = true
			out = append(out, m)
		}
		writeJSON(w, http.StatusOK, out)
	}
}
