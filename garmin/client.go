// Package garmin 是 Garmin Connect Personal API 的客户端（票 15 的首个云同步平台）。
//
// 认证方式（官方 Personal API，非 OAuth 三脚流程）：
//
//	用户在 Garmin Connect 网页生成 Personal API Key（30天-1年有效）
//	  → POST /personalapitoken-service/personal-api/personal-api-key/token
//	     Header: Authorization: ApiKey <key>
//	  → 返回 OAuth2 access_token（JWT，24h 有效，无 refresh token，用 Key 重新换取）
//
// 数据端点：
//
//	GET /activitylist-service/activities/search/activities?start&limit&startDate&endDate&activityType
//	GET /download-service/files/activity/{id}   → 原始文件 ZIP（内含 FIT）
//
// 文档：https://developer.garmin.com/gc-developer-program/personal/personal-api/
// 端点路径核对自官方示例客户端 github.com/garmin/connect-personal-api-python。
package garmin

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// TokenPath Personal API Key 换 token 的端点路径。
	TokenPath = "/personalapitoken-service/personal-api/personal-api-key/token"
	// defaultBaseURL 官方 Personal API 主机。
	defaultBaseURL = "https://personal.connectapi.garmin.com"
	// pageSize 活动列表单页条数（官方示例默认 20，上限经验值 100）。
	pageSize = 100
)

// AuthError 认证失败（API Key 无效/过期，或 token 失效）。
type AuthError struct{ Msg string }

func (e *AuthError) Error() string { return "Garmin 认证失败: " + e.Msg }

// IsAuthError 判定是否为认证类错误（同步层据此提示用户更新 API Key）。
func IsAuthError(err error) bool {
	_, ok := err.(*AuthError)
	return ok
}

// APIError 非认证类的 API 错误。
type APIError struct {
	Status int
	Msg    string
}

func (e *APIError) Error() string { return fmt.Sprintf("Garmin API %d: %s", e.Status, e.Msg) }

// ActivityType 活动类型。
type ActivityType struct {
	TypeKey string `json:"typeKey"`
}

// Activity Garmin 活动列表条目（只取用到的字段）。
type Activity struct {
	ActivityID      int64        `json:"activityId"`
	ActivityName    string       `json:"activityName"`
	ActivityType    ActivityType `json:"activityType"`
	StartTimeLocal  string       `json:"startTimeLocal"` // "2026-05-01 07:00:00"（本地时间）
	Distance        float64      `json:"distance"`       // 米
	Duration        float64      `json:"duration"`       // 秒
	ElapsedDuration float64      `json:"elapsedDuration"`
}

// Sport 映射到 openrunner 的运动类型。
func (a Activity) Sport() string {
	switch strings.ToLower(a.ActivityType.TypeKey) {
	case "running", "trail_running", "treadmill_running", "track_running", "indoor_running":
		return "run"
	case "cycling", "road_biking", "mountain_biking", "gravel_cycling", "indoor_cycling", "virtual_ride":
		return "ride"
	case "lap_swimming", "open_water_swimming", "swimming":
		return "swim"
	default:
		return "other"
	}
}

// Client Garmin Personal API 客户端。
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewClient 创建客户端；baseURL 为空时用官方主机（测试可注入 httptest URL）。
func NewClient(baseURL, apiKey string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// ExchangeToken 用 Personal API Key 换取 OAuth2 access token（24h 有效，无 refresh token）。
func (c *Client) ExchangeToken(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+TokenPath, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "ApiKey "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求 token 失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", &AuthError{Msg: "API Key 无效或已过期，请在 Garmin Connect 重新生成"}
	}
	if resp.StatusCode != http.StatusOK {
		return "", &APIError{Status: resp.StatusCode, Msg: truncate(string(body), 200)}
	}

	var out struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("解析 token 响应失败: %w", err)
	}
	if out.AccessToken == "" {
		return "", &APIError{Status: resp.StatusCode, Msg: "token 响应缺少 access_token"}
	}
	return out.AccessToken, nil
}

// ListActivitiesSince 拉取 startDate（含）之后的活动；自动分页直到取完。
func (c *Client) ListActivitiesSince(ctx context.Context, token string, since time.Time) ([]Activity, error) {
	var all []Activity
	for start := 0; ; start += pageSize {
		page, err := c.listActivitiesPage(ctx, token, since, start)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < pageSize {
			break
		}
	}
	return all, nil
}

func (c *Client) listActivitiesPage(ctx context.Context, token string, since time.Time, start int) ([]Activity, error) {
	q := url.Values{}
	q.Set("start", fmt.Sprintf("%d", start))
	q.Set("limit", fmt.Sprintf("%d", pageSize))
	q.Set("startDate", since.Format("2006-01-02"))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/activitylist-service/activities/search/activities?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("拉活动列表失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, &AuthError{Msg: "access token 已失效"}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{Status: resp.StatusCode, Msg: truncate(string(body), 200)}
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, nil // 官方客户端对空 body 返回 nil
	}
	var out []Activity
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("解析活动列表失败: %w", err)
	}
	return out, nil
}

// DownloadActivityFIT 下载活动原始文件（ZIP）并解出其中的 FIT 字节。
func (c *Client) DownloadActivityFIT(ctx context.Context, token string, activityID int64) ([]byte, error) {
	if activityID <= 0 {
		return nil, fmt.Errorf("activityID 必须为正")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/download-service/files/activity/%d", c.baseURL, activityID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/zip, application/x-zip-compressed, application/octet-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载活动文件失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, &AuthError{Msg: "access token 已失效"}
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, &APIError{Status: resp.StatusCode, Msg: truncate(string(body), 200)}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20)) // 单活动上限 64MB
	if err != nil {
		return nil, fmt.Errorf("读取下载内容失败: %w", err)
	}
	return extractFITFromZip(raw)
}

// extractFITFromZip 从 ZIP 中取第一个 .fit 文件（Garmin 的 activity-download 返回 ZIP）。
func extractFITFromZip(raw []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		// 非 ZIP：可能是直接返回的 FIT
		if len(raw) > 12 && string(raw[8:12]) == ".FIT" {
			return raw, nil
		}
		return nil, fmt.Errorf("下载内容既非 ZIP 也非 FIT: %w", err)
	}
	for _, f := range zr.File {
		if strings.HasSuffix(strings.ToLower(f.Name), ".fit") {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(io.LimitReader(rc, 64<<20))
		}
	}
	return nil, fmt.Errorf("ZIP 中没有 .fit 文件")
}

// TokenExpiry 从 JWT 的 payload 解析 exp（不校验签名——仅用于缓存判断）。
func TokenExpiry(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return time.Time{}, false
	}
	return time.Unix(claims.Exp, 0), true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
