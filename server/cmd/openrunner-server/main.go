// Command openrunner-server 是 openrunner 的 HTTP 服务入口（ADR-0002）。
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/openrunner/core"

	"github.com/openrunner/server/api"
	"github.com/openrunner/server/store"
	"github.com/openrunner/server/web"
)

var version = "dev"

func main() {
	var (
		dataDir = flag.String("data", defaultDataDir(), "数据目录（SQLite 库所在位置）")
		addr    = flag.String("addr", ":8080", "监听地址")
	)
	flag.Parse()

	dbPath := filepath.Join(*dataDir, "openrunner.db")
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		log.Fatalf("创建数据目录失败: %v", err)
	}

	// OPENRUNNER_DB_DSN 非空 → MySQL；否则 SQLite（默认，见 ADR-0002）
	db, err := store.OpenFromEnv(dbPath)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("关闭数据库失败: %v", err)
		}
	}()

	// 领域库连通性冒烟：证明 server→core 依赖成立（walking skeleton）
	if err := (core.Athlete{Name: "default"}).Validate(); err != nil {
		log.Fatalf("领域库不可用: %v", err)
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           api.NewMux(db.Gorm(), version, web.Handler()),
		ReadHeaderTimeout: 5 * time.Second,
	}
	// 云同步定时任务（票 15）；OPENRUNNER_SYNC_ENABLED=false 可关闭
	if getenv("OPENRUNNER_SYNC_ENABLED") != "false" {
		startScheduler(db.Gorm())
	}

	log.Printf("openrunner %s listening on %s (data: %s)", version, *addr, dbPath)
	log.Fatal(srv.ListenAndServe())
}

func defaultDataDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".openrunner")
	}
	return "./data"
}

func getenv(key string) string {
	return os.Getenv(key)
}
