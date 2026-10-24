// Package store 封装数据库访问（GORM）。
// 当前驱动固定 SQLite（纯 Go，无 cgo）；MySQL 兼容由后续票经环境变量接入（ADR-0002 目标态）。
package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/glebarez/sqlite"
)

// DB 是应用侧的数据库句柄。
type DB struct {
	gorm *gorm.DB
}

// Gorm 暴露底层 *gorm.DB 供 api 层查询。
func (d *DB) Gorm() *gorm.DB { return d.gorm }

// Close 关闭底层连接池。
func (d *DB) Close() error {
	sqlDB, err := d.gorm.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Open 打开 SQLite 数据库、执行迁移并初始化应用密钥。
func Open(path string) (*DB, error) {
	g, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}
	if err := g.AutoMigrate(&UserModel{}, &SecretModel{}, &AthleteModel{}, &ActivityModel{}, &ActivityStreamModel{}); err != nil {
		return nil, fmt.Errorf("迁移失败: %w", err)
	}
	d := &DB{gorm: g}
	if err := d.ensureJWTSecret(); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *DB) ensureJWTSecret() error {
	var count int64
	if err := d.gorm.Model(&SecretModel{}).Where("key = ?", secretKeyJWT).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	return d.gorm.Create(&SecretModel{Key: secretKeyJWT, Value: hex.EncodeToString(raw)}).Error
}
