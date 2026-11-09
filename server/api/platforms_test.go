package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Garmin 平台连接与同步（票 15）：S2 fixture 回放——用 httptest 模拟 Personal API。

func fakeGarminAPI(t *testing.T, opts struct {
	apiKey         string
	bearer         string
	activities     []map[string]any
	fitBytes       []byte
	downloadStatus int
}) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /personalapitoken-service/personal-api/personal-api-key/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "ApiKey "+opts.apiKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": opts.bearer, "token_type": "Bearer", "expires_in": 86400})
	})
	mux.HandleFunc("GET /activitylist-service/activities/search/activities", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+opts.bearer {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(opts.activities)
	})
	mux.HandleFunc("GET /download-service/files/activity/{id}", func(w http.ResponseWriter, r *http.Request) {
		if opts.downloadStatus != 0 {
			w.WriteHeader(opts.downloadStatus)
			return
		}
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		f, _ := zw.Create("activity.fit")
		_, _ = f.Write(opts.fitBytes)
		_ = zw.Close()
		_, _ = w.Write(buf.Bytes())
	})
	return httptest.NewServer(mux)
}

func TestGarminPlatformFlow(t *testing.T) {
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)

	fitBytes, err := os.ReadFile("../../core/garmin/testdata/sample.fit")
	if err != nil {
		t.Fatalf("读 fixture 失败: %v", err)
	}

	fake := fakeGarminAPI(t, struct {
		apiKey         string
		bearer         string
		activities     []map[string]any
		fitBytes       []byte
		downloadStatus int
	}{
		apiKey: "garmin-key-1", bearer: "bearer-1", fitBytes: fitBytes,
		activities: []map[string]any{
			{"activityId": 1001, "activityName": "Morning Run", "activityType": map[string]any{"typeKey": "running"}, "startTimeLocal": "2026-05-01 07:00:00", "distance": 10000},
		},
	})
	defer fake.Close()
	old := garminBaseURL
	garminBaseURL = fake.URL
	defer func() { garminBaseURL = old }()

	t.Run("保存凭证（加密，不回显）", func(t *testing.T) {
		w := do(h, http.MethodPut, "/api/athletes/1/platforms/garmin", token, `{"apiKey":"garmin-key-1"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("期望 201, 得到 %d: %s", w.Code, w.Body.String())
		}
		var p struct {
			Platform      string `json:"platform"`
			HasCredential bool   `json:"hasCredential"`
			Enabled       bool   `json:"enabled"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &p)
		if p.Platform != "garmin" || !p.HasCredential || !p.Enabled {
			t.Fatalf("期望 garmin+凭据+启用, 得到 %+v", p)
		}
		// 响应的 JSON 中不得出现明文 key
		if strings.Contains(w.Body.String(), "garmin-key-1") {
			t.Fatal("凭证明文出现在响应中")
		}
	})

	t.Run("列出连接（不含凭证）", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/platforms", token, "")
		var list []map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &list)
		if len(list) != 1 {
			t.Fatalf("期望 1 条, 得到 %d", len(list))
		}
		if _, has := list[0]["credentialEnc"]; has {
			t.Fatal("响应不应含 credentialEnc 字段")
		}
	})

	t.Run("同步：拉列表 → 下载 ZIP→FIT → 入库", func(t *testing.T) {
		w := do(h, http.MethodPost, "/api/athletes/1/platforms/garmin/sync", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			Imported int    `json:"imported"`
			Skipped  int    `json:"skipped"`
			Failed   int    `json:"failed"`
			Status   string `json:"status"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res.Status != "ok" || res.Imported != 1 {
			t.Fatalf("期望 ok + 1 导入, 得到 %+v", res)
		}
	})

	t.Run("再次同步幂等（content_hash 去重）", func(t *testing.T) {
		w := do(h, http.MethodPost, "/api/athletes/1/platforms/garmin/sync", token, "")
		var res struct {
			Imported int `json:"imported"`
			Skipped  int `json:"skipped"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res.Imported != 0 || res.Skipped != 1 {
			t.Fatalf("期望 0 导入 1 跳过, 得到 %+v", res)
		}
	})

	t.Run("游标已推进", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/platforms", token, "")
		var list []struct {
			SyncCursor string `json:"syncCursor"`
			LastSyncAt int64  `json:"lastSyncAt"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &list)
		if list[0].SyncCursor == "" || list[0].LastSyncAt == 0 {
			t.Fatalf("期望游标与同步时间已记录, 得到 %+v", list[0])
		}
	})

	t.Run("凭证失效路径（API Key 错误 → 可读错误）", func(t *testing.T) {
		if w := do(h, http.MethodPut, "/api/athletes/1/platforms/garmin", token, `{"apiKey":"wrong"}`); w.Code != http.StatusCreated {
			t.Fatal(w.Code)
		}
		w := do(h, http.MethodPost, "/api/athletes/1/platforms/garmin/sync", token, "")
		if w.Code != http.StatusBadGateway {
			t.Fatalf("期望 502, 得到 %d", w.Code)
		}
		var res struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if !strings.Contains(res.Message, "API Key") {
			t.Errorf("期望提示 API Key 问题, 得到 %q", res.Message)
		}
		// 恢复正确 key
		do(h, http.MethodPut, "/api/athletes/1/platforms/garmin", token, `{"apiKey":"garmin-key-1"}`)
	})

	t.Run("summary-only 路径（下载失败只计数不中断）", func(t *testing.T) {
		fake2 := fakeGarminAPI(t, struct {
			apiKey         string
			bearer         string
			activities     []map[string]any
			fitBytes       []byte
			downloadStatus int
		}{
			apiKey: "garmin-key-1", bearer: "bearer-1", fitBytes: fitBytes,
			downloadStatus: http.StatusNotFound, // 模拟该活动无原始文件
			activities: []map[string]any{
				{"activityId": 2002, "activityType": map[string]any{"typeKey": "cycling"}, "startTimeLocal": "2026-05-02 08:00:00"},
			},
		})
		defer fake2.Close()
		garminBaseURL = fake2.URL
		w := do(h, http.MethodPost, "/api/athletes/1/platforms/garmin/sync", token, "")
		var res struct {
			Imported int `json:"imported"`
			Failed   int `json:"failed"`
			Status   string
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res.Failed != 1 || res.Status != "ok" {
			t.Fatalf("期望 1 失败但整体 ok, 得到 %+v", res)
		}
	})

	t.Run("删除连接", func(t *testing.T) {
		if w := do(h, http.MethodDelete, "/api/athletes/1/platforms/garmin", token, ""); w.Code != http.StatusNoContent {
			t.Fatalf("期望 204, 得到 %d", w.Code)
		}
		if w := do(h, http.MethodPost, "/api/athletes/1/platforms/garmin/sync", token, ""); w.Code != http.StatusBadGateway {
			t.Fatalf("删除后同步应失败, 得到 %d", w.Code)
		}
	})

	t.Run("不支持的平台 400", func(t *testing.T) {
		// 用真正未实现的平台名（strava / intervals / coros 现在都已接入）
		if w := do(h, http.MethodPut, "/api/athletes/1/platforms/nosuchplatform", token, `{"apiKey":"x"}`); w.Code != http.StatusBadRequest {
			t.Fatalf("期望 400, 得到 %d", w.Code)
		}
	})
}

// 国行 garmin.cn 路径（票 15 扩展）：OAuth1 凭据 → 换 OAuth2 → cn 端点拉数据。
func TestGarminCNOAuth1Flow(t *testing.T) {
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)

	fitBytes, err := os.ReadFile("../../core/garmin/testdata/sample.fit")
	if err != nil {
		t.Fatalf("读 fixture: %v", err)
	}

	// 模拟 garmin.cn：exchange/user/2.0 + 活动列表 + 下载
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth-service/oauth/exchange/user/2.0", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "OAuth ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "cn-oauth2-token", "refresh_token": "cn-r", "expires_in": 3600,
		})
	})
	mux.HandleFunc("GET /activitylist-service/activities/search/activities", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cn-oauth2-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"activityId": 9001, "activityType": map[string]any{"typeKey": "running"}, "startTimeLocal": "2026-06-01 07:00:00"},
		})
	})
	mux.HandleFunc("GET /download-service/files/activity/{id}", func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		f, _ := zw.Create("a.fit")
		_, _ = f.Write(fitBytes)
		_ = zw.Close()
		_, _ = w.Write(buf.Bytes())
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	old := garminBaseURL
	garminBaseURL = srv.URL
	defer func() { garminBaseURL = old }()

	t.Run("保存 OAuth1 凭据（region=cn）", func(t *testing.T) {
		w := do(h, http.MethodPut, "/api/athletes/1/platforms/garmin", token,
			`{"region":"cn","oauth1Token":"o1-token","oauth1Secret":"o1-secret"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("期望 201, 得到 %d: %s", w.Code, w.Body.String())
		}
		var p struct {
			Region         string `json:"region"`
			CredentialKind string `json:"credentialKind"`
			HasCredential  bool   `json:"hasCredential"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &p)
		if p.Region != "cn" || p.CredentialKind != "oauth1" || !p.HasCredential {
			t.Fatalf("期望 cn/oauth1/有凭据, 得到 %+v", p)
		}
		if strings.Contains(w.Body.String(), "o1-token") || strings.Contains(w.Body.String(), "o1-secret") {
			t.Fatal("凭证明文泄漏到响应")
		}
	})

	t.Run("同步：OAuth1 → OAuth2 → cn 端点 → 入库", func(t *testing.T) {
		w := do(h, http.MethodPost, "/api/athletes/1/platforms/garmin/sync", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			Imported int    `json:"imported"`
			Status   string `json:"status"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res.Status != "ok" || res.Imported != 1 {
			t.Fatalf("期望 ok + 1 导入, 得到 %+v", res)
		}
	})

	t.Run("OAuth1 失效 → 可读错误", func(t *testing.T) {
		failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer failSrv.Close()
		garminBaseURL = failSrv.URL
		w := do(h, http.MethodPost, "/api/athletes/1/platforms/garmin/sync", token, "")
		if w.Code != http.StatusBadGateway {
			t.Fatalf("期望 502, 得到 %d", w.Code)
		}
		var res struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if !strings.Contains(res.Message, "OAuth1") {
			t.Errorf("期望提示 OAuth1 凭据, 得到 %q", res.Message)
		}
		garminBaseURL = srv.URL
	})
}
