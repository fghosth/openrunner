package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fghosth/openrunner/server/store"
)

// newTestServer 组装一个临时库 + 完整路由的测试服务，返回处理器与底层 DB（供构造过期 token 等用例）。
func newTestServer(t *testing.T) (http.Handler, *store.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Logf("关闭测试库失败: %v", err)
		}
	})
	return NewMux(db.Gorm(), "test", nil), db
}

func do(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func setupAdmin(t *testing.T, h http.Handler) string {
	t.Helper()
	w := do(h, http.MethodPost, "/api/auth/setup", "", `{"username":"admin","password":"secret123"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("setup 失败: %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return body.Token
}

func TestFirstRunSetup(t *testing.T) {
	h, _ := newTestServer(t)

	t.Run("未设账密时 status 报告需要设置", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/auth/status", "", "")
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d", w.Code)
		}
		var body struct {
			NeedsSetup bool `json:"needsSetup"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if !body.NeedsSetup {
			t.Fatal("期望 needsSetup=true")
		}
	})

	t.Run("setup 创建管理员并签发 token", func(t *testing.T) {
		w := do(h, http.MethodPost, "/api/auth/setup", "", `{"username":"admin","password":"secret123"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("期望 201, 得到 %d: %s", w.Code, w.Body.String())
		}
		var body struct {
			Token string `json:"token"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body.Token == "" {
			t.Fatal("期望返回 token")
		}
	})

	t.Run("已设账密后 setup 被 409 拒绝", func(t *testing.T) {
		w := do(h, http.MethodPost, "/api/auth/setup", "", `{"username":"evil","password":"x123456"}`)
		if w.Code != http.StatusConflict {
			t.Fatalf("期望 409, 得到 %d", w.Code)
		}
	})

	t.Run("设置后 status 报告无需设置", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/auth/status", "", "")
		var body struct {
			NeedsSetup bool `json:"needsSetup"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body.NeedsSetup {
			t.Fatal("期望 needsSetup=false")
		}
	})
}

func TestLogin(t *testing.T) {
	h, db := newTestServer(t)
	setupAdmin(t, h)

	t.Run("正确账密返回 token", func(t *testing.T) {
		w := do(h, http.MethodPost, "/api/auth/login", "", `{"username":"admin","password":"secret123"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d", w.Code)
		}
		var body struct {
			Token string `json:"token"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body.Token == "" {
			t.Fatal("期望 token")
		}
	})

	t.Run("错误密码被 401 拒绝", func(t *testing.T) {
		w := do(h, http.MethodPost, "/api/auth/login", "", `{"username":"admin","password":"wrong"}`)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("期望 401, 得到 %d", w.Code)
		}
	})

	t.Run("未知用户被 401 拒绝", func(t *testing.T) {
		w := do(h, http.MethodPost, "/api/auth/login", "", `{"username":"nobody","password":"x12345"}`)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("期望 401, 得到 %d", w.Code)
		}
	})

	t.Run("过期的 token 被 401 拒绝", func(t *testing.T) {
		expired, err := store.SignExpiredTokenForTest(db, time.Now().Add(-time.Hour))
		if err != nil {
			t.Fatalf("构造过期 token 失败: %v", err)
		}
		w := do(h, http.MethodGet, "/api/athletes", expired, "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("期望 401, 得到 %d", w.Code)
		}
	})
}

func TestAuthRequired(t *testing.T) {
	h, _ := newTestServer(t)

	t.Run("无 token 访问受保护 API 被拒", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes", "", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("期望 401, 得到 %d", w.Code)
		}
	})

	t.Run("伪造 token 被拒", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes", "fake.token.here", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("期望 401, 得到 %d", w.Code)
		}
	})
}

func TestSetupValidation(t *testing.T) {
	h, _ := newTestServer(t)

	cases := []struct {
		name, body string
		want       int
	}{
		{"缺少用户名", `{"password":"x123456"}`, http.StatusBadRequest},
		{"密码过短", `{"username":"a","password":"12345"}`, http.StatusBadRequest},
		{"非法 JSON", `{`, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := do(h, http.MethodPost, "/api/auth/setup", "", c.body)
			if w.Code != c.want {
				t.Fatalf("期望 %d, 得到 %d (%s)", c.want, w.Code, w.Body.String())
			}
		})
	}
}
