// web 包内嵌 UI 静态资源（openrunner-ui 构建产物，见 Makefile ui-dist 目标）。
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
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		served := path
		if _, err := fs.Stat(sub, path); err != nil {
			// SPA 回退：实际要发的是 index.html，缓存头也得按它来——
			// 否则一个不存在的 /assets/x.js 会把 index.html 标成永久缓存。
			r.URL.Path = "/"
			served = "index.html"
		}
		if strings.HasPrefix(served, "assets/") {
			w.Header().Set("Cache-Control", immutableCache)
		} else {
			w.Header().Set("Cache-Control", revalidateCache)
		}
		fileServer.ServeHTTP(w, r)
	})
}
