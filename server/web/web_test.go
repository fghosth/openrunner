package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// 测试自带内存 dist，不读内嵌产物。
//
// 为什么：内嵌 dist 在 fresh clone 里只有 .gitkeep（构建产物不入库，见 .gitignore），
// 于是 Handler() 走「产物缺失」的 503 分支，这几个测试在 CI 上全红——
// 而其中一个用 t.Skip 躲过，等于那条缓存规则在 CI 上从来没被验证过。
// 依赖构建产物的测试不是集成测试，只是碰巧本地跑过。
func fakeDist() fstest.MapFS {
	text := func(s string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte(s), Mode: 0o444}
	}
	return fstest.MapFS{
		"index.html":              text("<!doctype html><title>openrunner</title>"),
		"favicon.svg":             text("<svg/>"),
		"assets/index-abc123.js":  text("console.log(1)"),
		"assets/index-abc123.css": text("body{}"),
	}
}

func TestHashedAssetsAreCachedForever(t *testing.T) {
	for _, asset := range []string{"/assets/index-abc123.js", "/assets/index-abc123.css"} {
		rec := httptest.NewRecorder()
		spaHandler(fakeDist()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, asset, nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("%s 状态码 = %d，期望 200", asset, rec.Code)
		}
		cc := rec.Header().Get("Cache-Control")
		if cc == "" {
			t.Fatalf("%s 没有 Cache-Control，浏览器每次访问都要重下（实测 2.6 MB）", asset)
		}
		if !strings.Contains(cc, "max-age=31536000") || !strings.Contains(cc, "immutable") {
			t.Errorf("%s 的 Cache-Control=%q，期望带 max-age=31536000 与 immutable", asset, cc)
		}
	}
}

func TestIndexIsNeverCached(t *testing.T) {
	// index.html 一旦缓存，用户就再也拿不到新版本——它必须每次回源校验。
	for _, path := range []string{"/", "/index.html", "/map", "/trends"} {
		rec := httptest.NewRecorder()
		spaHandler(fakeDist()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("%s 状态码 = %d，期望 200", path, rec.Code)
		}
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
	spaHandler(fakeDist()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/activities/7", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("前端路由状态码 = %d，期望 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "openrunner") {
		t.Errorf("回退应发出 index.html 内容，实际：%q", rec.Body.String())
	}
	// 回退发的是 index.html，缓存头必须跟着它，不能按请求路径标成永久缓存
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-cache") {
		t.Errorf("回退响应的 Cache-Control=%q，期望 no-cache", cc)
	}
}

func TestMissingAssetDoesNotPoisonCache(t *testing.T) {
	// 不存在的 /assets/x.js 走回退，绝不能被标成 assets 的永久缓存
	rec := httptest.NewRecorder()
	spaHandler(fakeDist()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/does-not-exist.js", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望回退到 index.html 的 200", rec.Code)
	}
	cc := rec.Header().Get("Cache-Control")
	if strings.Contains(cc, "immutable") {
		t.Errorf("不存在的 assets 路径被标成永久缓存（%q）——回退后应按 index.html 计", cc)
	}
}

func TestMissingDistGivesActionableError(t *testing.T) {
	// dist 里连 index.html 都没有（fresh clone 没跑 make ui-dist）：
	// 必须给明确指引，而不是白屏 + 一堆 404。
	rec := httptest.NewRecorder()
	missingAssetsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("状态码 = %d，期望 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "make ui-dist") {
		t.Errorf("提示应包含修复命令，实际：%q", rec.Body.String())
	}
}

func TestHandlerNeverPanicsWithoutDist(t *testing.T) {
	// 内嵌 dist 在 fresh clone 里只有 .gitkeep —— Handler 不能 panic，
	// 必须返回可操作的 503（否则整个进程起不来，比白屏更难诊断）。
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Handler() panic：%v", r)
		}
	}()
	h := Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	// 有产物 → 200；没产物 → 503。两者都是合法终态，不能是 404/500
	if rec.Code != http.StatusOK && rec.Code != http.StatusServiceUnavailable {
		t.Errorf("状态码 = %d，期望 200 或 503", rec.Code)
	}
}
