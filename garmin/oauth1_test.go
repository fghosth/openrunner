package garmin

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

// OAuth1 → OAuth2 交换（票 15 扩展：国行 garmin.cn 支持）。
// 用户只需提供 oauth1 token/secret（长期有效），Go 端自行换取 24h oauth2 token。

// fakeExchange 模拟 POST /oauth-service/oauth/exchange/user/2.0。
// 它会独立重算 OAuth1 签名并比对，从而验证我们签名算法的正确性。
func fakeExchange(t *testing.T, wantConsumerKey, wantConsumerSecret, wantToken, wantTokenSecret string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth-service/oauth/exchange/user/2.0" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "OAuth ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		params := parseOAuthHeader(strings.TrimPrefix(auth, "OAuth "))
		for _, k := range []string{"oauth_consumer_key", "oauth_nonce", "oauth_signature",
			"oauth_signature_method", "oauth_timestamp", "oauth_token", "oauth_version"} {
			if params[k] == "" {
				t.Errorf("Authorization 缺少 %s", k)
			}
		}
		if params["oauth_consumer_key"] != wantConsumerKey {
			t.Errorf("consumer_key 不符: %s", params["oauth_consumer_key"])
		}
		if params["oauth_token"] != wantToken {
			t.Errorf("oauth_token 不符: %s", params["oauth_token"])
		}
		if params["oauth_signature_method"] != "HMAC-SHA1" {
			t.Errorf("签名方法应为 HMAC-SHA1, 得到 %s", params["oauth_signature_method"])
		}

		// 独立重算签名验证正确性
		got := params["oauth_signature"]
		delete(params, "oauth_signature")
		want := refSignOAuth1("POST", "http://"+r.Host+r.URL.Path, params, wantConsumerSecret, wantTokenSecret)
		if got != want {
			t.Errorf("签名不符\n got=%s\nwant=%s", got, want)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "oauth2-access-token",
			"refresh_token": "oauth2-refresh-token",
			"token_type":    "Bearer",
			"expires_in":    3600,
			"scope":         "",
		})
	}))
}

// parseOAuthHeader 解析 `k="v", k2="v2"` 形式。
func parseOAuthHeader(s string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		// RFC 5849：header 内的参数值需 percent-encode，解析时要解码
		v, err := url.QueryUnescape(strings.Trim(kv[1], `"`))
		if err != nil {
			v = strings.Trim(kv[1], `"`)
		}
		out[kv[0]] = v
	}
	return out
}

// refSignOAuth1 独立实现（测试侧真值），与生产实现对比验证。
func refSignOAuth1(method, baseURL string, params map[string]string, consumerSecret, tokenSecret string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var pairs []string
	for _, k := range keys {
		pairs = append(pairs, refPercentEncode(k)+"="+refPercentEncode(params[k]))
	}
	baseString := method + "&" + refPercentEncode(baseURL) + "&" + refPercentEncode(strings.Join(pairs, "&"))
	key := refPercentEncode(consumerSecret) + "&" + refPercentEncode(tokenSecret)
	mac := hmac.New(sha1.New, []byte(key))
	mac.Write([]byte(baseString))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func refPercentEncode(s string) string {
	e := url.QueryEscape(s)
	e = strings.ReplaceAll(e, "+", "%20")
	e = strings.ReplaceAll(e, "*", "%2A")
	e = strings.ReplaceAll(e, "%7E", "~")
	return e
}

func TestExchangeOAuth2(t *testing.T) {
	const ck, cs = "test-consumer-key", "test-consumer-secret"
	const tok, tsec = "user-oauth1-token", "user-oauth1-secret"

	srv := fakeExchange(t, ck, cs, tok, tsec)
	defer srv.Close()

	c := NewClient("", "") // baseURL 由 ExchangeOAuth2 参数决定
	c.baseURL = srv.URL
	c.consumerKey = ck
	c.consumerSecret = cs

	t.Run("oauth1 → oauth2 交换（签名被服务端独立重算验证）", func(t *testing.T) {
		tok2, err := c.ExchangeOAuth2(context.Background(), tok, tsec)
		if err != nil {
			t.Fatalf("交换失败: %v", err)
		}
		if tok2.AccessToken != "oauth2-access-token" {
			t.Fatalf("期望 oauth2-access-token, 得到 %q", tok2.AccessToken)
		}
		if tok2.RefreshToken != "oauth2-refresh-token" {
			t.Errorf("期望 refresh token, 得到 %q", tok2.RefreshToken)
		}
		if tok2.ExpiresIn != 3600 {
			t.Errorf("期望 3600, 得到 %d", tok2.ExpiresIn)
		}
	})

	t.Run("401 → AuthError", func(t *testing.T) {
		badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer badSrv.Close()
		c2 := NewClient("", "")
		c2.baseURL = badSrv.URL
		c2.consumerKey, c2.consumerSecret = ck, cs
		_, err := c2.ExchangeOAuth2(context.Background(), tok, tsec)
		if err == nil || !IsAuthError(err) {
			t.Fatalf("期望 AuthError, 得到 %v", err)
		}
	})
}

func TestRegionBaseURL(t *testing.T) {
	t.Run("国行端点", func(t *testing.T) {
		if got := BaseURLForRegion("cn"); got != "https://connectapi.garmin.cn" {
			t.Fatalf("期望 garmin.cn, 得到 %q", got)
		}
	})
	t.Run("国际端点", func(t *testing.T) {
		if got := BaseURLForRegion("global"); got != "https://connectapi.garmin.com" {
			t.Fatalf("期望 garmin.com, 得到 %q", got)
		}
	})
	t.Run("大小写与未知值", func(t *testing.T) {
		if BaseURLForRegion("CN") != "https://connectapi.garmin.cn" {
			t.Error("期望大小写不敏感")
		}
		if BaseURLForRegion("xxx") != "https://connectapi.garmin.com" {
			t.Error("未知区域应回退国际")
		}
	})
}

func TestOAuth2Expiry(t *testing.T) {
	tok := OAuth2Token{AccessToken: "a", RefreshToken: "r", ExpiresIn: 3600, ObtainedAt: time.Now()}
	if !tok.Valid() {
		t.Fatal("刚获得应有效")
	}
	tok.ObtainedAt = time.Now().Add(-2 * time.Hour)
	if tok.Valid() {
		t.Fatal("过期后应无效")
	}
}
