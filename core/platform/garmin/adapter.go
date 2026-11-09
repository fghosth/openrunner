// Package garmin 把已有的 garmin 客户端适配成 platform.Provider。
//
// 这是「不重构现有代码也能接入统一协议」的示例：garmin 包（上层目录）
// 已经有完整实现与测试，这里只做一层薄适配，不动原代码。
package garmin

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/fghosth/openrunner/core/garmin"
	"github.com/fghosth/openrunner/core/platform"
)

const name = "garmin"

// Credential 字段名（两种认证方式，见 garmin 包）。
const (
	FieldRegion         = "region"         // "global" | "cn"
	FieldCredentialKind = "credentialKind" // "api_key" | "oauth1"
	FieldAPIKey         = "apiKey"
	FieldOAuth1Token    = "oauth1Token"
	FieldOAuth1Secret   = "oauth1Secret"
)

func init() { platform.Register(Provider{}) }

// Provider 实现 platform.Provider（薄适配层）。
type Provider struct{}

func (Provider) Name() string { return name }

func (Provider) Validate(cred platform.Credential) error {
	kind := cred.Get(FieldCredentialKind)
	switch kind {
	case "oauth1":
		return cred.Require(FieldOAuth1Token, FieldOAuth1Secret)
	case "api_key", "":
		return cred.Require(FieldAPIKey)
	default:
		return fmt.Errorf("未知的凭证类型: %s", kind)
	}
}

// client 构造底层客户端并换取 access token。
func (p Provider) client(ctx context.Context, cred platform.Credential) (*garmin.Client, string, error) {
	region := cred.Get(FieldRegion)
	if region == "" {
		region = "global"
	}
	// 测试注入：platform.SetBaseURL("garmin", httptestURL) 后走该地址；
	// 生产为空 → 由 region 决定真实主机。
	// api_key 模式要把 key 交给客户端（它在 ExchangeToken 里用）
	apiKey := ""
	if cred.Get(FieldCredentialKind) != "oauth1" {
		apiKey = cred.Get(FieldAPIKey)
	}
	if override := platform.BaseURL(name, ""); override != "" {
		c := garmin.NewClient(override, apiKey)
		token, err := exchangeToken(ctx, c, cred)
		if err != nil {
			return nil, "", err
		}
		return c, token, nil
	}
	c := garmin.NewClientForRegion(region, apiKey)
	token, err := exchangeToken(ctx, c, cred)
	if err != nil {
		return nil, "", err
	}
	return c, token, nil
}

// exchangeToken 按凭证类型换取 access token（API Key 或 OAuth1）。
func exchangeToken(ctx context.Context, c *garmin.Client, cred platform.Credential) (string, error) {
	var token string
	if cred.Get(FieldCredentialKind) == "oauth1" {
		tok, err := c.ExchangeOAuth2(ctx, cred.Get(FieldOAuth1Token), cred.Get(FieldOAuth1Secret))
		if err != nil {
			if garmin.IsAuthError(err) {
				return "", &platform.AuthError{Msg: "OAuth1 凭据失效，请重新运行辅助脚本"}
			}
			return "", fmt.Errorf("换取 OAuth2 token 失败: %w", err)
		}
		token = tok.AccessToken
	} else {
		tok, err := c.ExchangeToken(ctx)
		if err != nil {
			if garmin.IsAuthError(err) {
				return "", &platform.AuthError{Msg: "API Key 无效或已过期"}
			}
			return "", fmt.Errorf("换取 token 失败: %w", err)
		}
		token = tok
	}
	return token, nil
}

func (p Provider) ListActivities(ctx context.Context, cred platform.Credential, since time.Time) ([]platform.ActivityRef, error) {
	if err := p.Validate(cred); err != nil {
		return nil, err
	}
	c, token, err := p.client(ctx, cred)
	if err != nil {
		return nil, err
	}
	list, err := c.ListActivitiesSince(ctx, token, since)
	if err != nil {
		if garmin.IsAuthError(err) {
			return nil, &platform.AuthError{Msg: "access token 失效"}
		}
		return nil, fmt.Errorf("拉取活动列表失败: %w", err)
	}
	out := make([]platform.ActivityRef, 0, len(list))
	for _, a := range list {
		t, _ := time.ParseInLocation("2006-01-02 15:04:05", a.StartTimeLocal, time.Local)
		out = append(out, platform.ActivityRef{
			ID:        strconv.FormatInt(a.ActivityID, 10),
			StartTime: t,
			Sport:     platform.Sport(a.Sport()),
			Name:      a.ActivityName,
		})
	}
	return out, nil
}

func (p Provider) Download(ctx context.Context, cred platform.Credential, ref platform.ActivityRef) ([]byte, string, error) {
	if err := p.Validate(cred); err != nil {
		return nil, "", err
	}
	c, token, err := p.client(ctx, cred)
	if err != nil {
		return nil, "", err
	}
	id, err := strconv.ParseInt(ref.ID, 10, 64)
	if err != nil {
		return nil, "", fmt.Errorf("非法的活动 id: %s", ref.ID)
	}
	data, err := c.DownloadActivityFIT(ctx, token, id)
	if err != nil {
		return nil, "", fmt.Errorf("下载活动文件失败: %w", err)
	}
	// Garmin 客户端已解压 ZIP，直接返回 FIT
	return data, "garmin-" + ref.ID + ".fit", nil
}

// 保证 json 被使用（Credential 是 map[string]string，无需额外解析）
var _ = json.Marshal
