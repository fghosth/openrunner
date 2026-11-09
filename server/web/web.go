// web 包内嵌 UI 静态资源（web/（前端）构建产物，见 Makefile ui-dist 目标）。
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// 缓存策略。Vite 给 /assets 下的产物文件名打了内容 hash——同名必同内容，
// 所以可以永久缓存；反过来 index.html 一旦缓存，用户就永远拿不到新版本，
// 必须每次回源校验。实测未加头时每次访问重下 2.6 MB JS + 100 KB CSS。
const (
	immutableCache  = "public, max-age=31536000, immutable"
	revalidateCache = "no-cache"
)

// Handler 返回 SPA 静态资源处理器：未知路径回退到 index.html。
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic("web/dist 缺失：先运行 make ui-dist")
	}
	// fresh clone 没跑 make ui-dist 时 dist 里只有 .gitkeep——
	// 给明确指引，而不是白屏 + 一堆 404（debt-sprint 票 05）
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return missingAssetsHandler()
	}
	return spaHandler(sub)
}

// missingAssetsHandler 是前端产物缺失时的明确提示。
func missingAssetsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("前端产物缺失：请先运行 make ui-dist 再 make build（Docker 构建同理）。\n"))
	})
}

// spaHandler 是纯函数：给它一个含 index.html 的 FS 就返回处理器。
//
// 单独拆出来是为了让测试自带内存 FS（fstest.MapFS），不依赖 make ui-dist 的产物。
// 之前四个测试直接打 Handler()，fresh clone（也就是 CI 的形态）里 dist 只有
// .gitkeep，于是全部撞上「产物缺失」的 503 而红；其中一个还 t.Skip 蒙混过去，
// 等于那条缓存在 CI 上从来没验过。
func spaHandler(sub fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		served := path
		switch {
		case path == "index.html":
			// http.FileServer 会把 /index.html 301 到 ./，白跑一个来回。
			// 这里已经知道文件存在，直接按根路径发。
			r.URL.Path = "/"
		default:
			if _, err := fs.Stat(sub, path); err != nil {
				// SPA 回退：实际要发的是 index.html，缓存头也得按它来——
				// 否则一个不存在的 /assets/x.js 会把 index.html 标成永久缓存。
				r.URL.Path = "/"
				served = "index.html"
			}
		}
		if strings.HasPrefix(served, "assets/") {
			w.Header().Set("Cache-Control", immutableCache)
		} else {
			w.Header().Set("Cache-Control", revalidateCache)
		}
		fileServer.ServeHTTP(w, r)
	})
}
