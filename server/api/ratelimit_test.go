package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeClock 让窗口测试不用真等 60s（票 06 验收：可注入时钟）。
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }
func (c *fakeClock) advance(d time.Duration) {
	c.t = c.t.Add(d)
}

func TestRateLimiterTokenBucket(t *testing.T) {
	c := &fakeClock{t: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)}
	l := newRateLimiter(5, 5) // 5 次/分钟
	l.now = c.now

	for i := 0; i < 5; i++ {
		if _, ok := l.allow("1.2.3.4"); !ok {
			t.Fatalf("第 %d 次应放行（burst=5）", i+1)
		}
	}
	retry, ok := l.allow("1.2.3.4")
	if ok {
		t.Fatal("第 6 次应被拒")
	}
	if retry < 1 || retry > 12 {
		t.Fatalf("Retry-After 不合理：%d", retry)
	}
	// 不同 IP 互不影响
	if _, ok := l.allow("5.6.7.8"); !ok {
		t.Fatal("其他 IP 不应被连坐")
	}
	// 补一个令牌需要 12s（5/min）
	c.advance(12 * time.Second)
	if _, ok := l.allow("1.2.3.4"); !ok {
		t.Fatal("12s 后应恢复 1 次")
	}
	// 满窗恢复
	c.advance(60 * time.Second)
	for i := 0; i < 5; i++ {
		if _, ok := l.allow("1.2.3.4"); !ok {
			t.Fatalf("满窗后 burst 应恢复，第 %d 次被拒", i+1)
		}
	}
	// reset（登录成功）立即清零
	l.reset("1.2.3.4")
	if _, ok := l.allow("1.2.3.4"); !ok {
		t.Fatal("reset 后应立即可用")
	}
}

func TestLoginRateLimitHTTP(t *testing.T) {
	h, _ := newTestServer(t)
	token := setupAdmin(t, h)
	_ = createAthlete(t, h, token)

	body := `{"username":"admin","password":"wrong-password"}`
	// 前 5 次失败尝试：401（账密错误）
	for i := 0; i < 5; i++ {
		w := doRaw(h, http.MethodPost, "/api/auth/login", body, map[string]string{"X-Forwarded-For": "9.9.9.9"})
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("第 %d 次应 401，得到 %d", i+1, w.Code)
		}
	}
	// 第 6 次：429 + Retry-After
	w := doRaw(h, http.MethodPost, "/api/auth/login", body, map[string]string{"X-Forwarded-For": "9.9.9.9"})
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("第 6 次应 429，得到 %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("429 应带 Retry-After")
	}
	// 其他 IP 不受影响；且正确密码仍可登录（成功清零欠账）
	if w := doRaw(h, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"`+"secret123"+`"}`,
		map[string]string{"X-Forwarded-For": "8.8.8.8"}); w.Code != http.StatusOK {
		t.Fatalf("其他 IP 正常登录应 200，得到 %d", w.Code)
	}
}

func TestSetupRateLimitHTTP(t *testing.T) {
	h, _ := newTestServer(t)
	// 第一次 setup 成功（201）
	if w := doRaw(h, http.MethodPost, "/api/auth/setup", `{"username":"a1","password":"passw0rd!"}`, nil); w.Code != http.StatusCreated {
		t.Fatalf("首次 setup 应 201，得到 %d", w.Code)
	}
	// setup 桶在成功时已清零，但 1 次/分钟 burst=1：再来一次失败尝试（409）后立刻限流
	if w := doRaw(h, http.MethodPost, "/api/auth/setup", `{"username":"a2","password":"passw0rd!"}`, nil); w.Code != http.StatusConflict {
		t.Fatalf("重复 setup 应 409，得到 %d", w.Code)
	}
	if w := doRaw(h, http.MethodPost, "/api/auth/setup", `{"username":"a3","password":"passw0rd!"}`, nil); w.Code != http.StatusTooManyRequests {
		t.Fatalf("第三次 setup 应 429，得到 %d", w.Code)
	}
}

// 正常登录不受限流影响（验收：成功清零，反复登录不累计）。
func TestLoginSuccessNotThrottled(t *testing.T) {
	h, _ := newTestServer(t)
	token := setupAdmin(t, h)
	_ = createAthlete(t, h, token)
	body := `{"username":"admin","password":"` + "secret123" + `"}`
	for i := 0; i < 10; i++ {
		w := doRaw(h, http.MethodPost, "/api/auth/login", body, nil)
		if w.Code != http.StatusOK {
			b := map[string]any{}
			_ = json.Unmarshal(w.Body.Bytes(), &b)
			t.Fatalf("第 %d 次正常登录应 200，得到 %d (%v)", i+1, w.Code, b)
		}
	}
}

// doRaw 与 do 同构但可加自定义头（XFF 分桶测试用）。
func doRaw(h http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
