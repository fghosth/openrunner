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
		if _, err := fs.Stat(sub, path); err != nil {
			// SPA 回退
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})
}
