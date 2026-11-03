package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 内嵌 dist 里随便挑一个 Vite 产物（文件名带内容 hash，因此可永久缓存）。
func anyHashedAsset(t *testing.T) string {
	t.Helper()
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		t.Fatalf("dist 缺失: %v", err)
	}
	var found string
	_ = fs.WalkDir(sub, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && (strings.HasSuffix(p, ".js") || strings.HasSuffix(p, ".css")) {
			found = "/" + p
			return fs.SkipAll
		}
		return nil
	})
	if found == "" {
		t.Skip("dist/assets 下没有 js/css，跳过（先 make ui-dist）")
	}
	return found
}

func TestHashedAssetsAreCachedForever(t *testing.T) {
	asset := anyHashedAsset(t)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, asset, nil))

	cc := rec.Header().Get("Cache-Control")
	if cc == "" {
		t.Fatalf("%s 没有 Cache-Control，浏览器每次访问都要重下（实测 2.6 MB）", asset)
	}
	if !strings.Contains(cc, "max-age=31536000") || !strings.Contains(cc, "immutable") {
		t.Errorf("%s 的 Cache-Control=%q，期望带 max-age=31536000 与 immutable", asset, cc)
	}
}

func TestIndexIsNeverCached(t *testing.T) {
	// index.html 一旦缓存，用户就再也拿不到新版本——它必须每次回源校验。
	for _, path := range []string{"/", "/index.html", "/map", "/trends"} {
		rec := httptest.NewRecorder()
		Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		cc := rec.Header().Get("Cache-Control")
		if strings.Contains(cc, "immutable") || strings.Contains(cc, "max-age=31536000") {
			t.Errorf("%s 被长缓存了（Cache-Control=%q），新版本将不可见", path, cc)
		}
		if !strings.Contains(cc, "no-cache") {
			t.Errorf("%s 的 Cache-Control=%q，期望含 no-cache", path, cc)
		}
	}
}

func TestSPAFallbackStillServesIndex(t *testing.T) {
	// 前端路由（/activities/7 之类）必须回退到 index.html，而不是 404。
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/activities/7", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("前端路由状态码 = %d，期望 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "<div id=\"root\">") {
		t.Error("前端路由没有回退到 index.html")
	}
}

func TestMissingAssetDoesNotPoisonCache(t *testing.T) {
	// 不存在的 /assets/x.js 会回退到 index.html；若按请求路径判缓存，
	// 就会把 HTML 标成 immutable，浏览器从此拿不到新版本。
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/does-not-exist-abc123.js", nil))

	cc := rec.Header().Get("Cache-Control")
	if strings.Contains(cc, "immutable") {
		t.Errorf("回退到 index.html 却带 immutable（Cache-Control=%q）", cc)
	}
	if !strings.Contains(cc, "no-cache") {
		t.Errorf("Cache-Control=%q，期望 no-cache", cc)
	}
}
