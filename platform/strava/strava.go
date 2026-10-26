// Package strava 接入 Strava（官方 API v3）。
//
// 认证：OAuth2。用户在 https://www.strava.com/settings/api 自助创建应用后，
// 拿到 client_id / client_secret，再走一次授权拿 refresh_token。
// 本实现只需要这三个字段 —— access_token 每次同步时用 refresh_token 换新的
// （Strava 的 access_token 只有 6 小时有效期）。
//
// 端点：
//
//	POST https://www.strava.com/oauth/token               （refresh_token → access_token）
//	GET  https://www.strava.com/api/v3/athlete/activities （列表，分页 200/页）
//	GET  https://www.strava.com/api/v3/activities/{id}     （详情，含 map）
//
// 注意：Strava 不提供「下载原始 FIT」的公开端点（export_original 需要
// activity:read_all + 特定权限，且只对 GPS 活动可用）。这里退而求其次：
// **用 streams 接口重建 TCX**，这是开源生态（如 strava-to-tcx）的通行做法。
package strava

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/openrunner/core/platform"
)

const (
	defaultAuthURL = "https://www.strava.com/oauth/token"
	defaultAPIURL  = "https://www.strava.com/api/v3"
	name           = "strava"
	pageSize       = 200
)

// Credential 字段名。
const (
	FieldClientID     = "clientId"
	FieldClientSecret = "clientSecret"
	FieldRefreshToken = "refreshToken"
)

func init() { platform.Register(Provider{}) }

// Provider 实现 platform.Provider。
type Provider struct{}

func (Provider) Name() string { return name }

func (Provider) Validate(cred platform.Credential) error {
	return cred.Require(FieldClientID, FieldClientSecret, FieldRefreshToken)
}

// token 是 OAuth 换回来的访问凭证。
type token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
}

// refresh 用 refresh_token 换 access_token。
func (p Provider) refresh(ctx context.Context, cred platform.Credential) (*token, error) {
	body, status, err := platform.HTTPPostForm(ctx, authEndpoint(), map[string]string{
		"client_id":     cred.Get(FieldClientID),
		"client_secret": cred.Get(FieldClientSecret),
		"refresh_token": cred.Get(FieldRefreshToken),
		"grant_type":    "refresh_token",
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("换取 access token 失败: %w", err)
	}
	if status == 400 || status == 401 {
		return nil, &platform.AuthError{Msg: "client_id/secret 或 refresh_token 无效"}
	}
	if status != 200 {
		return nil, fmt.Errorf("换取 access token 失败: HTTP %d: %s", status, truncate(string(body), 200))
	}
	var tk token
	if err := json.Unmarshal(body, &tk); err != nil {
		return nil, fmt.Errorf("解析 token 失败: %w", err)
	}
	if tk.AccessToken == "" {
		return nil, &platform.AuthError{Msg: "Strava 未返回 access_token"}
	}
	return &tk, nil
}

// apiActivity 列表接口的关键字段。
type apiActivity struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	SportType  string  `json:"sport_type"`
	StartDate  string  `json:"start_date_local"` // "2026-05-01T07:00:00Z"
	Distance   float64 `json:"distance"`
	MovingTime int     `json:"moving_time"`
}

// ListActivities 分页拉 since 之后的活动。
func (p Provider) ListActivities(ctx context.Context, cred platform.Credential, since time.Time) ([]platform.ActivityRef, error) {
	if err := p.Validate(cred); err != nil {
		return nil, err
	}
	tk, err := p.refresh(ctx, cred)
	if err != nil {
		return nil, err
	}

	var out []platform.ActivityRef
	after := since.Unix()
	for page := 1; page <= 50; page++ { // 上限 50 页 = 10000 条，防止死循环
		u := fmt.Sprintf("%s/athlete/activities?after=%d&per_page=%d&page=%d",
			apiEndpoint(), after, pageSize, page)
		body, status, err := platform.HTTPGet(ctx, u, map[string]string{
			"Authorization": "Bearer " + tk.AccessToken,
		})
		if err != nil {
			return nil, fmt.Errorf("拉活动列表失败: %w", err)
		}
		if status == 401 {
			return nil, &platform.AuthError{Msg: "access token 被拒（refresh_token 可能已撤销）"}
		}
		if status == 429 {
			return nil, fmt.Errorf("触发 Strava 限流（每 15 分钟 200 次），请稍后再试")
		}
		if status != 200 {
			return nil, fmt.Errorf("拉活动列表失败: HTTP %d", status)
		}
		var list []apiActivity
		if err := json.Unmarshal(body, &list); err != nil {
			return nil, fmt.Errorf("解析活动列表失败: %w", err)
		}
		for _, a := range list {
			st, _ := time.Parse("2006-01-02T15:04:05Z", a.StartDate)
			if st.IsZero() {
				st, _ = time.Parse("2006-01-02T15:04:05", a.StartDate)
			}
			out = append(out, platform.ActivityRef{
				ID:        strconv.FormatInt(a.ID, 10),
				StartTime: st.Local(),
				Sport:     mapSport(a.SportType, a.Type),
				Name:      a.Name,
			})
		}
		if len(list) < pageSize {
			break
		}
	}
	return out, nil
}

// streamSet 是 Strava streams 接口返回的通道集合。
type streamSet struct {
	Type string    `json:"type"`
	Data []float64 `json:"data"`
}

// Download 用 streams 接口重建 TCX（Strava 无公开的原始文件下载端点）。
func (p Provider) Download(ctx context.Context, cred platform.Credential, ref platform.ActivityRef) ([]byte, string, error) {
	if err := p.Validate(cred); err != nil {
		return nil, "", err
	}
	tk, err := p.refresh(ctx, cred)
	if err != nil {
		return nil, "", err
	}

	// 详情（取开始时间 + 名称）
	detailURL := fmt.Sprintf("%s/activities/%s", apiEndpoint(), url.PathEscape(ref.ID))
	dBody, dStatus, err := platform.HTTPGet(ctx, detailURL, map[string]string{
		"Authorization": "Bearer " + tk.AccessToken,
	})
	if err != nil {
		return nil, "", fmt.Errorf("获取活动详情失败: %w", err)
	}
	if dStatus == 401 {
		return nil, "", &platform.AuthError{Msg: "access token 被拒"}
	}
	if dStatus != 200 {
		return nil, "", fmt.Errorf("获取活动详情失败: HTTP %d", dStatus)
	}
	var detail apiActivity
	_ = json.Unmarshal(dBody, &detail)

	// 逐点数据
	keys := "time,latlng,distance,altitude,heartrate,cadence,velocity_smooth,watts"
	sURL := fmt.Sprintf("%s/activities/%s/streams?keys=%s&key_by_type=true",
		apiEndpoint(), url.PathEscape(ref.ID), url.QueryEscape(keys))
	sBody, sStatus, err := platform.HTTPGet(ctx, sURL, map[string]string{
		"Authorization": "Bearer " + tk.AccessToken,
	})
	if err != nil {
		return nil, "", fmt.Errorf("获取逐点数据失败: %w", err)
	}
	if sStatus != 200 {
		return nil, "", fmt.Errorf("获取逐点数据失败: HTTP %d（该活动可能没有 GPS/心率数据）", sStatus)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(sBody, &raw); err != nil {
		return nil, "", fmt.Errorf("解析逐点数据失败: %w", err)
	}

	get := func(k string) []float64 {
		r, ok := raw[k]
		if !ok {
			return nil
		}
		var s streamSet
		if err := json.Unmarshal(r, &s); err != nil {
			return nil
		}
		return s.Data
	}
	// latlng 是 [[lat,lng],...]，单独解析
	var latlng [][2]float64
	if r, ok := raw["latlng"]; ok {
		var s struct {
			Data [][2]float64 `json:"data"`
		}
		_ = json.Unmarshal(r, &s)
		latlng = s.Data
	}

	tcx, err := buildTCX(detail, ref, get("time"), get("distance"), get("altitude"),
		get("heartrate"), get("cadence"), get("velocity_smooth"), get("watts"), latlng)
	if err != nil {
		return nil, "", err
	}
	return tcx, "strava-" + ref.ID + ".tcx", nil
}

// authEndpoint / apiEndpoint 支持测试注入（platform.SetBaseURL("strava", …)）。
func authEndpoint() string { return platform.BaseURL(name, defaultAuthURL) }
func apiEndpoint() string  { return platform.BaseURL(name, defaultAPIURL) }

func mapSport(sportType, typ string) platform.Sport {
	s := strings.ToLower(sportType)
	if s == "" {
		s = strings.ToLower(typ)
	}
	switch s {
	case "run", "trailrun", "virtualrun":
		return platform.SportRun
	case "ride", "virtualride", "gravelride", "mountainbikeride", "ebikeride":
		return platform.SportRide
	case "swim":
		return platform.SportSwim
	default:
		return platform.SportOther
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
