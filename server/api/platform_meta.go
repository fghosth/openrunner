package api

import (
	"net/http"

	"github.com/openrunner/core/platform"
	"gorm.io/gorm"
)

// platformField 是凭证表单里的一个输入项。
type platformField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Type        string `json:"type"` // text | password | select
	Placeholder string `json:"placeholder,omitempty"`
	Help        string `json:"help,omitempty"`
	Options     []struct {
		Value string `json:"value"`
		Label string `json:"label"`
	} `json:"options,omitempty"`
}

// platformMeta 是一个平台的元信息（供前端动态渲染凭证表单）。
type platformMeta struct {
	Name           string          `json:"name"`
	Label          string          `json:"label"`
	Help           string          `json:"help"`
	Fields         []platformField `json:"fields"`
	SetupURL       string          `json:"setupUrl,omitempty"`
	CredentialJSON bool            `json:"credentialJson"` // 凭证是否以 JSON 存储（新平台都是）
}

func opt(v, l string) struct {
	Value string `json:"value"`
	Label string `json:"label"`
} {
	return struct {
		Value string `json:"value"`
		Label string `json:"label"`
	}{Value: v, Label: l}
}

// platformMetas 是各平台的 UI 元信息。
//
// 放在 api 层而不是 platform 包：这些是**给人看的文案**，
// 平台包只负责协议（保持可移植、无 UI 耦合）。
func platformMetas() []platformMeta {
	return []platformMeta{
		{
			Name: "garmin", Label: "Garmin", SetupURL: "https://connect.garmin.com/modern/settings/account",
			Help: "国际站用 Personal API Key；中国站（garmin.cn）用 OAuth1 token/secret（由 scripts/garmin-cn-token.py 生成）",
			Fields: []platformField{
				{Key: "region", Label: "站点", Type: "select", Options: []struct {
					Value string `json:"value"`
					Label string `json:"label"`
				}{opt("global", "国际站（API Key）"), opt("cn", "中国站（OAuth1）")}},
				{Key: "apiKey", Label: "Personal API Key", Type: "password", Placeholder: "国际站用", Help: "Garmin Connect → 设置 → 账户 里生成"},
				{Key: "oauth1Token", Label: "OAuth1 token", Type: "password", Placeholder: "中国站用"},
				{Key: "oauth1Secret", Label: "OAuth1 secret", Type: "password", Placeholder: "中国站用"},
			},
		},
		{
			Name: "intervals", Label: "Intervals.icu", SetupURL: "https://intervals.icu/settings",
			Help: "所有平台里最省事的：只要一个 personal API key，无需注册应用",
			Fields: []platformField{
				{Key: "athleteId", Label: "Athlete ID", Type: "text", Placeholder: "i12345", Help: "intervals.icu 个人页 URL 里的 i 开头编号"},
				{Key: "apiKey", Label: "API Key", Type: "password", Help: "Settings → Developer → API Key"},
			},
		},
		{
			Name: "strava", Label: "Strava", SetupURL: "https://www.strava.com/settings/api",
			Help: "需先在 Strava 自助创建应用拿 client_id / secret，再走一次授权拿 refresh_token（access token 6 小时过期，会自动刷新）",
			Fields: []platformField{
				{Key: "clientId", Label: "Client ID", Type: "text"},
				{Key: "clientSecret", Label: "Client Secret", Type: "password"},
				{Key: "refreshToken", Label: "Refresh Token", Type: "password", Help: "首次授权后获得，长期有效"},
			},
		},
		{
			Name: "coros", Label: "COROS（高驰）",
			Help: "用 COROS App 的账号密码登录（非官方接口，若失败请改用文件导入）",
			Fields: []platformField{
				{Key: "account", Label: "账号", Type: "text", Placeholder: "手机号或邮箱"},
				{Key: "password", Label: "密码", Type: "password"},
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
