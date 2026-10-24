// Package store 封装数据库访问（GORM）。
// 当前驱动固定 SQLite（纯 Go，无 cgo）；MySQL 兼容由后续票经环境变量接入（ADR-0002 目标态）。
package store

import (
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/glebarez/sqlite"
)

// Open 打开 SQLite 数据库并执行自动迁移（当前为基线迁移，表随后续票增加）。
func Open(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		return nil, err
	}
	return db, nil
}

// migrate 建立基线 schema；后续票在此追加建表。
func migrate(db *gorm.DB) error {
	return db.AutoMigrate()
}
