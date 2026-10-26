// Package intervals 接入 Intervals.icu（https://intervals.icu）。
//
// 认证：HTTP Basic，用户名固定字符串 "API_KEY"，密码是用户在
// intervals.icu → Settings → Developer 生成的 personal API key。
// 这是所有支持平台里接入成本最低的一个 —— 用户不需要注册应用，只要复制一个 key。
//
// 端点（官方文档 https://intervals.icu/api-docs.html）：
//
//	GET /api/v1/athlete/{id}/activities?oldest=&newest=
//	GET /api/v1/activity/{id}/file            （原始文件，通常 .fit.gz 或 .fit）
package intervals

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/openrunner/core/platform"
)

const (
	defaultBaseURL = "https://intervals.icu/api/v1"
	// name 平台标识（存库与路由用）。
	name = "intervals"
)

// Credential 字段名。
const (
	FieldAthleteID = "athleteId" // 形如 i12345
	FieldAPIKey    = "apiKey"
)

func init() { platform.Register(Provider{}) }

// Provider 实现 platform.Provider。
type Provider struct{}

func (Provider) Name() string { return name }

func (Provider) Validate(cred platform.Credential) error {
	return cred.Require(FieldAthleteID, FieldAPIKey)
}

// apiActivity 是活动列表接口返回的最小字段集。
type apiActivity struct {
	ID        string `json:"id"`               // 形如 i12345678
	StartDate string `json:"start_date_local"` // "2026-05-01T07:00:00"
	Type      string `json:"type"`             // Run / Ride / VirtualRun …
	Name      string `json:"name"`
	Source    string `json:"source"`
	FileType  string `json:"file_type"`
}

// ListActivities 拉 since 之后的活动。
func (p Provider) ListActivities(ctx context.Context, cred platform.Credential, since time.Time) ([]platform.ActivityRef, error) {
	if err := p.Validate(cred); err != nil {
		return nil, err
	}
	athlete := cred.Get(FieldAthleteID)
	oldest := since.Format("2006-01-02")
	newest := time.Now().Format("2006-01-02")
	u := fmt.Sprintf("%s/athlete/%s/activities?oldest=%s&newest=%s",
		apiBase(), url.PathEscape(athlete), oldest, newest)

	body, status, err := platform.HTTPGet(ctx, u, map[string]string{
		"Authorization": basicAuth(cred.Get(FieldAPIKey)),
		"Accept":        "application/json",
	})
	if err != nil {
		return nil, fmt.Errorf("拉活动列表失败: %w", err)
	}
	if status == 401 || status == 403 {
		return nil, &platform.AuthError{Msg: "API key 无效或已过期"}
	}
	if status != 200 {
		return nil, fmt.Errorf("拉活动列表失败: HTTP %d", status)
	}
	var list []apiActivity
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("解析活动列表失败: %w", err)
	}
	out := make([]platform.ActivityRef, 0, len(list))
	for _, a := range list {
		if a.ID == "" {
			continue
		}
		st, _ := time.ParseInLocation("2006-01-02T15:04:05", a.StartDate, time.Local)
		out = append(out, platform.ActivityRef{
			ID:        a.ID,
			StartTime: st,
			Sport:     mapSport(a.Type),
			Name:      a.Name,
		})
	}
	return out, nil
}

// Download 下载原始文件。Intervals 的 /file 端点返回的可能是 .fit.gz 或 .fit，
// 这里统一解压（ingest 只吃 FIT/TCX/GPX 明文）。
func (p Provider) Download(ctx context.Context, cred platform.Credential, ref platform.ActivityRef) ([]byte, string, error) {
	if err := p.Validate(cred); err != nil {
		return nil, "", err
	}
	u := fmt.Sprintf("%s/activity/%s/file", apiBase(), url.PathEscape(ref.ID))
	body, status, err := platform.HTTPGet(ctx, u, map[string]string{
		"Authorization": basicAuth(cred.Get(FieldAPIKey)),
	})
	if err != nil {
		return nil, "", fmt.Errorf("下载活动文件失败: %w", err)
	}
	if status == 401 || status == 403 {
		return nil, "", &platform.AuthError{Msg: "API key 无效或已过期"}
	}
	if status != 200 {
		return nil, "", fmt.Errorf("下载活动文件失败: HTTP %d", status)
	}
	data, ext, err := platform.NormalizeOriginal(body)
	if err != nil {
		return nil, "", err
	}
	return data, name + "-" + sanitizeID(ref.ID) + ext, nil
}

// apiBase 返回 API 根地址（测试可通过 platform.SetBaseURL("intervals", …) 覆盖）。
func apiBase() string { return platform.BaseURL(name, defaultBaseURL) }

func basicAuth(key string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte("API_KEY:"+key))
}

func sanitizeID(id string) string {
	return strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(id)
}

// mapSport 把 Intervals 的类型映射到归一化运动类型。
func mapSport(t string) platform.Sport {
	switch strings.ToLower(t) {
	case "run", "virtualrun", "trailrun":
		return platform.SportRun
	case "ride", "virtualride", "gravelride", "mountainbikeride", "ebikeride":
		return platform.SportRide
	case "swim", "openwaterswim":
		return platform.SportSwim
	default:
		return platform.SportOther
	}
}
