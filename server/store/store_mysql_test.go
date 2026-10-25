package store

import (
	"os"
	"testing"
)

// MySQL 兼容性测试（票 13）：需外部 MySQL，未设置 DSN 时跳过。
// CI 用 services: mysql 提供，本地开发者可 docker run 后设置该变量。
// 用法：OPENRUNNER_TEST_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/openrunner_test?parseTime=true' go test ./store/
func TestMySQLCompatibility(t *testing.T) {
	dsn := os.Getenv("OPENRUNNER_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("未设置 OPENRUNNER_TEST_MYSQL_DSN，跳过 MySQL 兼容性测试")
	}

	db, err := OpenDSN(dsn)
	if err != nil {
		t.Fatalf("MySQL 连接失败: %v", err)
	}
	defer func() { _ = db.Close() }()

	// 迁移应在 MySQL 上成功（含 partial index 的方言处理）
	t.Run("迁移与基本 CRUD", func(t *testing.T) {
		a := AthleteModel{Name: "mysql-test-athlete"}
		if err := db.Gorm().Create(&a).Error; err != nil {
			t.Fatalf("插入档案失败: %v", err)
		}
		var got AthleteModel
		if err := db.Gorm().First(&got, a.ID).Error; err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if got.Name != "mysql-test-athlete" {
			t.Errorf("期望名称回读, 得到 %q", got.Name)
		}
		if err := db.Gorm().Delete(&got).Error; err != nil {
			t.Fatalf("删除失败: %v", err)
		}
	})

	t.Run("BLOB 往返（Stream 二进制）", func(t *testing.T) {
		a := AthleteModel{Name: "blob-test"}
		if err := db.Gorm().Create(&a).Error; err != nil {
			t.Fatal(err)
		}
		defer db.Gorm().Delete(&a)

		act := ActivityModel{AthleteID: a.ID, Sport: "run", StartTime: 1, HasStream: true}
		if err := db.Gorm().Create(&act).Error; err != nil {
			t.Fatalf("活动插入失败: %v", err)
		}
		blob := []byte(`{"recIntervalSecs":1,"hr":[100,101]}`)
		sm := ActivityStreamModel{ActivityID: act.ID, RecIntervalSecs: 1, Data: blob}
		if err := db.Gorm().Create(&sm).Error; err != nil {
			t.Fatalf("Stream 插入失败: %v", err)
		}
		var got ActivityStreamModel
		if err := db.Gorm().First(&got, "activity_id = ?", act.ID).Error; err != nil {
			t.Fatalf("Stream 读取失败: %v", err)
		}
		if string(got.Data) != string(blob) {
			t.Errorf("BLOB 往返不一致: %q", string(got.Data))
		}
		db.Gorm().Delete(&act)
		db.Gorm().Delete(&got)
	})
}
