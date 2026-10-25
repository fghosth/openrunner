package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
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

// Open 打开 SQLite 数据库（默认）、执行迁移并初始化应用密钥。
func Open(path string) (*DB, error) {
	g, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}
	return finishOpen(g)
}

// OpenDSN 按 DSN 打开 MySQL（票 13：环境变量切换）。
// DSN 形如 user:pass@tcp(host:3306)/dbname?parseTime=true&charset=utf8mb4
func OpenDSN(dsn string) (*DB, error) {
	g, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}
	return finishOpen(g)
}

// OpenFromEnv 按 OPENRUNNER_DB_DSN 选择驱动：为空 → SQLite(path)；非空 → MySQL。
func OpenFromEnv(path string) (*DB, error) {
	if dsn := strings.TrimSpace(os.Getenv("OPENRUNNER_DB_DSN")); dsn != "" {
		return OpenDSN(dsn)
	}
	return Open(path)
}

func finishOpen(g *gorm.DB) (*DB, error) {
	if err := g.AutoMigrate(&UserModel{}, &SecretModel{}, &AthleteModel{}, &ActivityModel{},
		&ActivityStreamModel{}, &HrZoneModel{}, &ActivityMetricsModel{}, &PaceZoneModel{},
		&SeasonModel{}, &CsZoneModel{}); err != nil {
		return nil, fmt.Errorf("迁移失败: %w", err)
	}
	if err := ensureIndexes(g); err != nil {
		return nil, fmt.Errorf("建索引失败: %w", err)
	}
	d := &DB{gorm: g}
	if err := d.ensureJWTSecret(); err != nil {
		return nil, err
	}
	return d, nil
}

// ensureIndexes 建方言相关的索引（票 13）。
// SQLite：partial unique index（content_hash 为空的手工活动不受约束）。
// MySQL：不支持 partial index，改用普通唯一索引 + 空串转 NULL 语义由写入侧保证
// （手工活动 content_hash 写 NULL 而非 ”，MySQL 唯一索引允许多个 NULL）。
func ensureIndexes(g *gorm.DB) error {
	dialect := g.Dialector.Name()
	if dialect == "mysql" {
		// MySQL 不支持 CREATE INDEX IF NOT EXISTS：先查 information_schema
		var count int64
		if err := g.Raw(`SELECT COUNT(*) FROM information_schema.statistics
			WHERE table_schema = DATABASE() AND table_name = 'activities'
			  AND index_name = 'idx_activity_athlete_hash'`).Scan(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		return g.Exec("CREATE UNIQUE INDEX idx_activity_athlete_hash ON activities (athlete_id, content_hash)").Error
	}
	return g.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_activity_athlete_hash ON activities (athlete_id, content_hash) WHERE content_hash != ''").Error
}

func (d *DB) ensureJWTSecret() error {
	var count int64
	if err := d.gorm.Model(&SecretModel{}).Where("secret_key = ?", secretKeyJWT).Count(&count).Error; err != nil {
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
