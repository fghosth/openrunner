package main

import (
	"log"
	"time"

	"github.com/openrunner/core/garmin"
	"github.com/openrunner/server/api"
	"github.com/openrunner/server/store"
	"gorm.io/gorm"
)

// startScheduler 启动云同步定时任务（票 15：cron 触发）。
// 用简单 ticker 而非 cron 表达式库：默认每 6 小时跑一次所有启用连接，
// 间隔可用 OPENRUNNER_SYNC_INTERVAL 覆盖（Go duration 格式，如 "30m"、"6h"）。
func startScheduler(db *gorm.DB) {
	interval := 6 * time.Hour
	if v := envDuration("OPENRUNNER_SYNC_INTERVAL"); v > 0 {
		interval = v
	}
	log.Printf("云同步调度已启动：每 %v 尝试一次（OPENRUNNER_SYNC_INTERVAL 可调）", interval)

	go func() {
		// 启动后先等 2 分钟（避免与应用启动争资源），之后按间隔跑
		time.Sleep(2 * time.Minute)
		for {
			runAllSyncs(db)
			time.Sleep(interval)
		}
	}()
}

func runAllSyncs(db *gorm.DB) {
	var conns []store.PlatformConnectionModel
	if err := db.Where("enabled = ?", true).Find(&conns).Error; err != nil {
		log.Printf("云同步：读取连接失败: %v", err)
		return
	}
	for _, c := range conns {
		if c.Platform != "garmin" {
			continue
		}
		res := api.TriggerPlatformSync(db, c.AthleteID, c.Platform)
		log.Printf("云同步 %s/%d: status=%s imported=%d skipped=%d failed=%d %s",
			c.Platform, c.AthleteID, res.Status, res.Imported, res.Skipped, res.Failed, res.Message)
	}
}

func envDuration(key string) time.Duration {
	// 避免额外依赖：手写解析（只支持标准 duration 字符串）
	if v := getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		log.Printf("忽略无效的 %s=%q（需 Go duration 格式，如 6h）", key, v)
	}
	return 0
}

var _ = garmin.NewClient
