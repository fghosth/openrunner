package garmin

// OAuth1 → OAuth2 交换（Garmin 国行 garmin.cn 支持，票 15 扩展）。
//
// 背景：Garmin 中国站目前不支持 Personal API（令牌交换 526），但官方网页/App 的
// 会话体系可用。用户用辅助脚本（garth）登录一次拿到 OAuth1 token/secret（长期有效），
// 本文件用 OAuth1 签名调 exchange/user/2.0 换 24h 的 OAuth2 token —— Go 端自带续期，
// 用户无需反复重跑脚本。
//
// OAuth consumer 常量来自 garth 公开托管的 oauth_consumer.json（非用户密文）；
// 协议细节核对自 garth 开源实现 github.com/matin/garth（sso.py）。
//
// 注意：国行 garmin.cn 使用自签/非标准证书链，调用方需允许不安全 TLS（仅限该域）。

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// OAuth consumer 常量（garth 公开托管：https://thegarth.s3.amazonaws.com/oauth_consumer.json）
const (
	garthConsumerKey    = "fc3e99d2-118c-44b8-8ae3-03370dde24c0"
	garthConsumerSecret = "E08WAR897WEy2knn7aFBrvegVAf0AFdWBBF"
	// garminMobileUA garth 使用的 Android UA（OAuth1 请求需与之匹配）
	garminMobileUA = "com.garmin.android.apps.connectmobile"
	// exchangePath OAuth1→OAuth2 交换端点路径
	exchangePath = "/oauth-service/oauth/exchange/user/2.0"
)

// 区域端点（票 15：国行/国际分流）。
const (
	baseURLCN     = "https://connectapi.garmin.cn"
	baseURLGlobal = "https://connectapi.garmin.com"
)

// BaseURLForRegion 返回区域对应的 API 主机（未知值回退国际）。
func BaseURLForRegion(region string) string {
	if strings.EqualFold(region, "cn") {
		return baseURLCN
	}
	return baseURLGlobal
}

// OAuth2Token OAuth2 访问令牌（24h 有效，可用 OAuth1 再换）。
type OAuth2Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	ObtainedAt   time.Time `json:"-"`
}

// Valid 令牌是否仍可用（留 5 分钟余量）。
func (t OAuth2Token) Valid() bool {
	if t.AccessToken == "" || t.ExpiresIn <= 0 {
		return false
	}
	return time.Now().Before(t.ObtainedAt.Add(time.Duration(t.ExpiresIn)*time.Second - 5*time.Minute))
}

// ExchangeOAuth2 用 OAuth1 token/secret 换取 OAuth2 token。
func (c *Client) ExchangeOAuth2(ctx context.Context, oauth1Token, oauth1Secret string) (*OAuth2Token, error) {
	if oauth1Token == "" || oauth1Secret == "" {
		return nil, &AuthError{Msg: "缺少 OAuth1 凭据"}
	}
	ck, cs := c.consumerKey, c.consumerSecret
	if ck == "" {
		ck, cs = garthConsumerKey, garthConsumerSecret
	}

	endpoint := c.baseURL + exchangePath
	params := map[string]string{
		"oauth_consumer_key":     ck,
		"oauth_nonce":            randomNonce(),
		"oauth_signature_method": "HMAC-SHA1",
		"oauth_timestamp":        fmt.Sprintf("%d", time.Now().Unix()),
		"oauth_token":            oauth1Token,
		"oauth_version":          "1.0",
	}
	params["oauth_signature"] = signOAuth1("POST", endpoint, params, cs, oauth1Secret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(""))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", oauth1Header(params))
	req.Header.Set("User-Agent", garminMobileUA)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OAuth1→OAuth2 交换请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, &AuthError{Msg: "OAuth1 凭据无效或已过期，请重新运行辅助脚本获取"}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{Status: resp.StatusCode, Msg: truncate(string(body), 200)}
	}

	out := &OAuth2Token{ObtainedAt: time.Now()}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("解析 OAuth2 响应失败: %w", err)
	}
	if out.AccessToken == "" {
		return nil, &APIError{Status: resp.StatusCode, Msg: "响应缺少 access_token"}
	}
	return out, nil
}

// signOAuth1 RFC 5849 HMAC-SHA1 签名。
func signOAuth1(method, endpoint string, params map[string]string, consumerSecret, tokenSecret string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "oauth_signature" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, percentEncode(k)+"="+percentEncode(params[k]))
	}
	baseString := method + "&" + percentEncode(endpoint) + "&" + percentEncode(strings.Join(pairs, "&"))
	key := percentEncode(consumerSecret) + "&" + percentEncode(tokenSecret)
	mac := hmac.New(sha1.New, []byte(key))
	mac.Write([]byte(baseString))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// oauth1Header 组装 Authorization: OAuth ... 头。
func oauth1Header(params map[string]string) string {
	keys := []string{"oauth_consumer_key", "oauth_nonce", "oauth_signature",
		"oauth_signature_method", "oauth_timestamp", "oauth_token", "oauth_version"}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, k, percentEncode(params[k])))
	}
	return "OAuth " + strings.Join(parts, ", ")
}

// percentEncode RFC 5849 百分号编码（与 url.QueryEscape 的差异：空格→%20、~ 不编码）。
func percentEncode(s string) string {
	e := url.QueryEscape(s)
	e = strings.ReplaceAll(e, "+", "%20")
	e = strings.ReplaceAll(e, "*", "%2A")
	e = strings.ReplaceAll(e, "%7E", "~")
	return e
}

func randomNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
