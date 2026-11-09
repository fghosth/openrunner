// Package coros 接入 COROS（高驰）。
//
// 认证：账号 + 密码 → accessToken（非官方 API，与 running_page 的做法一致：
// POST https://teamcnapi.coros.com/account/login，body 里的 pwd 传明文密码）。
// 这是国内跑者常用的平台，running_page 已验证多年。
//
// 端点（teamcnapi.coros.com）：
//
//	POST /account/login                     → data.accessToken
//	POST /activity/query                    → 活动列表（分页）
//	POST /activity/detail/download          → 原始文件（通常 FIT）
//
// 注意：COROS 未提供官方公开 API，接口可能变动；失败时应提示用户改用
// 文件导入（从 COROS App 导出 FIT）。
package coros

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/fghosth/openrunner/core/platform"
)

const (
	defaultBaseURL = "https://teamcnapi.coros.com"
	name           = "coros"
)

// Credential 字段名。
const (
	FieldAccount  = "account" // 手机号或邮箱
	FieldPassword = "password"
)

func init() { platform.Register(Provider{}) }

// Provider 实现 platform.Provider。
type Provider struct{}

func (Provider) Name() string { return name }

func (Provider) Validate(cred platform.Credential) error {
	return cred.Require(FieldAccount, FieldPassword)
}

// loginResp 登录响应。
type loginResp struct {
	Result  string `json:"result"`
	Message string `json:"message"`
	Data    struct {
		AccessToken string `json:"accessToken"`
		UserID      string `json:"userId"`
	} `json:"data"`
}

// login 账号密码换 accessToken。
func (p Provider) login(ctx context.Context, cred platform.Credential) (string, error) {
	payload, _ := json.Marshal(map[string]any{
		"account":     cred.Get(FieldAccount),
		"accountType": 2, // 2 = 手机/邮箱
		"pwd":         cred.Get(FieldPassword),
	})
	body, status, err := platform.HTTPPostJSON(ctx, apiBase()+"/account/login", payload, nil)
	if err != nil {
		return "", fmt.Errorf("COROS 登录失败: %w", err)
	}
	if status != 200 {
		return "", fmt.Errorf("COROS 登录失败: HTTP %d", status)
	}
	var r loginResp
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("解析登录响应失败: %w", err)
	}
	if r.Result != "0000" && r.Data.AccessToken == "" {
		msg := r.Message
		if msg == "" {
			msg = "账号或密码错误"
		}
		return "", &platform.AuthError{Msg: msg}
	}
	return r.Data.AccessToken, nil
}

// apiBase 返回 API 根地址（测试可通过 platform.SetBaseURL("coros", …) 覆盖）。
func apiBase() string { return platform.BaseURL(name, defaultBaseURL) }

// authHeaders COROS 用 cookie 传 token。
func authHeaders(token string) map[string]string {
	return map[string]string{
		"accesstoken": token,
		"Cookie":      "CPL-coros-region=2; CPL-coros-token=" + token,
		"Accept":      "application/json",
	}
}

// listResp 活动列表响应。
type listResp struct {
	Result  string `json:"result"`
	Message string `json:"message"`
	Data    struct {
		Count    int `json:"count"`
		DataList []struct {
			LabelID   string  `json:"labelId"`
			StartTime int64   `json:"startTime"` // 毫秒时间戳
			Name      string  `json:"name"`
			SportType int     `json:"sportType"`
			Distance  float64 `json:"distance"`
		} `json:"dataList"`
	} `json:"data"`
}

// ListActivities 分页拉活动（COROS 的 size 上限 20）。
func (p Provider) ListActivities(ctx context.Context, cred platform.Credential, since time.Time) ([]platform.ActivityRef, error) {
	if err := p.Validate(cred); err != nil {
		return nil, err
	}
	token, err := p.login(ctx, cred)
	if err != nil {
		return nil, err
	}

	var out []platform.ActivityRef
	const pageSize = 20
	for page := 1; page <= 100; page++ { // 上限 2000 条
		payload, _ := json.Marshal(map[string]any{
			"pageNumber": page,
			"pageSize":   pageSize,
		})
		body, status, err := platform.HTTPPostJSON(ctx, apiBase()+"/activity/query", payload, authHeaders(token))
		if err != nil {
			return nil, fmt.Errorf("拉活动列表失败: %w", err)
		}
		if status != 200 {
			return nil, fmt.Errorf("拉活动列表失败: HTTP %d", status)
		}
		var r listResp
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, fmt.Errorf("解析活动列表失败: %w", err)
		}
		if r.Result != "0000" {
			return nil, &platform.AuthError{Msg: r.Message}
		}
		reachedOlder := false
		for _, a := range r.Data.DataList {
			st := time.UnixMilli(a.StartTime).Local()
			if st.Before(since) {
				reachedOlder = true
				continue
			}
			out = append(out, platform.ActivityRef{
				ID:        a.LabelID,
				StartTime: st,
				Sport:     mapSportType(a.SportType),
				Name:      a.Name,
			})
		}
		if reachedOlder || len(r.Data.DataList) < pageSize {
			break
		}
	}
	return out, nil
}

// Download 下载原始文件（COROS 返回 FIT 的直链或字节流）。
func (p Provider) Download(ctx context.Context, cred platform.Credential, ref platform.ActivityRef) ([]byte, string, error) {
	if err := p.Validate(cred); err != nil {
		return nil, "", err
	}
	token, err := p.login(ctx, cred)
	if err != nil {
		return nil, "", err
	}
	payload, _ := json.Marshal(map[string]any{
		"labelId":  ref.ID,
		"fileType": 4, // 4 = FIT
	})
	body, status, err := platform.HTTPPostJSON(ctx, apiBase()+"/activity/detail/download", payload, authHeaders(token))
	if err != nil {
		return nil, "", fmt.Errorf("下载活动文件失败: %w", err)
	}
	if status != 200 {
		return nil, "", fmt.Errorf("下载活动文件失败: HTTP %d", status)
	}
	// COROS 可能返回 JSON（含下载直链）或直接的 FIT 字节
	var fileResp struct {
		Result  string `json:"result"`
		Message string `json:"message"`
		Data    struct {
			FileURL string `json:"fileUrl"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &fileResp) == nil && fileResp.Data.FileURL != "" {
		raw, st2, err := platform.HTTPGet(ctx, fileResp.Data.FileURL, nil)
		if err != nil {
			return nil, "", fmt.Errorf("下载活动文件失败: %w", err)
		}
		if st2 != 200 {
			return nil, "", fmt.Errorf("下载活动文件失败: HTTP %d", st2)
		}
		body = raw
	}
	data, ext, err := platform.NormalizeOriginal(body)
	if err != nil {
		return nil, "", fmt.Errorf("COROS 返回的文件无法解析（可用 App 导出 FIT 后手动导入）: %w", err)
	}
	return data, name + "-" + sanitize(ref.ID) + ext, nil
}

func sanitize(id string) string {
	return strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(id)
}

// mapSportType COROS 的运动类型编码（1=跑步 2=骑行 3=游泳 等）。
func mapSportType(t int) platform.Sport {
	switch t {
	case 1, 100: // 跑步 / 跑步机
		return platform.SportRun
	case 2, 200: // 骑行 / 室内骑行
		return platform.SportRide
	case 3, 300:
		return platform.SportSwim
	default:
		return platform.SportOther
	}
}
