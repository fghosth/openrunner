package garmin

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// Garmin Personal API 客户端（票 15）。用 httptest 模拟官方端点，
// 回放录制样式的响应——覆盖 流数据 / summary-only / 凭证失效 三路径（S2 缝）。

// fakeGarmin 模拟 personal.connectapi.garmin.com 的三类端点。
func fakeGarmin(t *testing.T, opts fakeOpts) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	// 1) token 端点
	mux.HandleFunc("POST /personalapitoken-service/personal-api/personal-api-key/token", func(w http.ResponseWriter, r *http.Request) {
		if opts.tokenFails {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
			return
		}
		if got := r.Header.Get("Authorization"); got != "ApiKey "+opts.apiKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		exp := time.Now().Add(24 * time.Hour).Unix()
		// 官方返回 JWT（带 exp claim）
		jwt := "eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjE3MDAwMDAwMDB9.sig"
		if opts.jwtExp != 0 {
			jwt = makeJWT(opts.jwtExp)
		} else {
			jwt = makeJWT(exp)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": jwt, "token_type": "Bearer", "expires_in": 86400,
		})
	})

	// 2) 活动列表
	mux.HandleFunc("GET /activitylist-service/activities/search/activities", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+opts.bearer {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(opts.activities)
	})

	// 3) 下载原始文件（ZIP 含 FIT）
	mux.HandleFunc("GET /download-service/files/activity/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+opts.bearer {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		f, err := zw.Create("123456789_ACTIVITY.fit")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(opts.fitBytes); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(buf.Bytes())
	})

	return httptest.NewServer(mux)
}

func authOK(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
}

// makeJWT 构造带 exp 的 JWT（标准 base64url 编码；仅测 payload 解析，签名不校验）。
func makeJWT(exp int64) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, _ := json.Marshal(map[string]any{"exp": exp})
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

type fakeOpts struct {
	apiKey     string
	bearer     string
	tokenFails bool
	jwtExp     int64
	activities []Activity
	fitBytes   []byte
}

func TestTokenExchange(t *testing.T) {
	opts := fakeOpts{apiKey: "test-key", bearer: "tok-1"}
	srv := fakeGarmin(t, opts)
	defer srv.Close()

	c := NewClient(srv.URL, opts.apiKey)

	t.Run("API Key 换 token", func(t *testing.T) {
		tok, err := c.ExchangeToken(context.Background())
		if err != nil {
			t.Fatalf("换 token 失败: %v", err)
		}
		if tok == "" {
			t.Fatal("期望非空 token")
		}
	})

	t.Run("错误 API Key → 401 可读错误", func(t *testing.T) {
		bad := NewClient(srv.URL, "wrong-key")
		_, err := bad.ExchangeToken(context.Background())
		if err == nil {
			t.Fatal("期望失败")
		}
		if !IsAuthError(err) {
			t.Fatalf("期望 AuthError, 得到 %T: %v", err, err)
		}
	})
}

func TestListActivities(t *testing.T) {
	opts := fakeOpts{
		apiKey: "k", bearer: "tok",
		activities: []Activity{
			{ActivityID: 1, ActivityName: "Morning Run", ActivityType: ActivityType{TypeKey: "running"}, StartTimeLocal: "2026-05-01 07:00:00", Distance: 10000},
			{ActivityID: 2, ActivityName: "Ride", ActivityType: ActivityType{TypeKey: "cycling"}, StartTimeLocal: "2026-05-02 08:00:00", Distance: 30000},
		},
	}
	srv := fakeGarmin(t, opts)
	defer srv.Close()

	c := NewClient(srv.URL, opts.apiKey)
	since := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	list, err := c.ListActivitiesSince(context.Background(), "tok", since)
	if err != nil {
		t.Fatalf("拉列表失败: %v", err)
	}
	if len(list) != 2 || list[0].ActivityID != 1 {
		t.Fatalf("期望 2 条活动, 得到 %+v", list)
	}
	if list[0].Sport() != "run" || list[1].Sport() != "ride" {
		t.Errorf("期望运动类型映射 run/ride, 得到 %q/%q", list[0].Sport(), list[1].Sport())
	}
}

func TestDownloadActivityFIT(t *testing.T) {
	// 用真实 FIT fixture 包进 ZIP，模拟 Garmin 的 activity-download
	fitBytes, err := readFixture("testdata/sample.fit")
	if err != nil {
		t.Skipf("无 fixture: %v", err)
	}
	opts := fakeOpts{apiKey: "k", bearer: "tok", fitBytes: fitBytes}
	srv := fakeGarmin(t, opts)
	defer srv.Close()

	c := NewClient(srv.URL, opts.apiKey)
	got, err := c.DownloadActivityFIT(context.Background(), "tok", 123456789)
	if err != nil {
		t.Fatalf("下载失败: %v", err)
	}
	if !bytes.Equal(got, fitBytes) {
		t.Fatalf("期望解出的 FIT 与原始一致, 得到 %d 字节 vs %d", len(got), len(fitBytes))
	}
}

// readFixture 读 testdata 下的二进制样本。
func readFixture(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func TestTokenExpiryParse(t *testing.T) {
	t.Run("解析 JWT exp", func(t *testing.T) {
		exp := time.Now().Add(24 * time.Hour).Unix()
		jwt := makeJWT(exp)
		got, ok := TokenExpiry(jwt)
		if !ok || got.Unix() != exp {
			t.Fatalf("期望解析出 exp=%d, 得到 %v ok=%v", exp, got, ok)
		}
	})
	t.Run("非 JWT → ok=false", func(t *testing.T) {
		if _, ok := TokenExpiry("not-a-jwt"); ok {
			t.Fatal("期望 false")
		}
	})
}
