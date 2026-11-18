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

	t.Run("赛历整表替换与省市区统计", func(t *testing.T) {
		rows := []RaceEventModel{
			raceRow("1", "广州马拉松", "2026-12-01", "广东省", "广州市", "天河区", "full,half"),
			raceRow("2", "深圳半马", "2026-11-01", "广东省", "深圳市", "", "half"),
			raceRow("3", "南京 10K", "2026-10-01", "江苏省", "南京市", "", "10k"),
		}
		if _, err := ReplaceRaceEvents(db.Gorm(), rows); err != nil {
			t.Fatalf("整表替换失败: %v", err)
		}
		// 再替换一次：MySQL 上「先删后插同事务」与唯一键的交互和 SQLite 不完全一样
		if _, err := ReplaceRaceEvents(db.Gorm(), rows); err != nil {
			t.Fatalf("二次整表替换失败: %v", err)
		}
		sum, err := RaceSummary(db.Gorm(), RaceFilter{})
		if err != nil {
			t.Fatal(err)
		}
		if sum.Total != 3 || len(sum.Provinces) != 2 {
			t.Errorf("summary 不对: total=%d provinces=%d", sum.Total, len(sum.Provinces))
		}
		list, err := ListRaceEvents(db.Gorm(), RaceFilter{Province: "广东省", Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || list[0].Name != "广州马拉松" {
			t.Errorf("按省 + limit 不对: %+v", list)
		}
	})

	t.Run("报名信息整表替换与批量挂载", func(t *testing.T) {
		rows := []RaceRegistrationModel{
			{RaceID: "1", Source: "zuicool", SourceID: "84480", RegEnd: "2026-11-18T12:00:00", MinPrice: 170},
			{RaceID: "2", Source: "zuicool", SourceID: "90001", RegStart: "2026-11-22T18:00:00"},
		}
		if _, err := ReplaceRaceRegistrations(db.Gorm(), rows); err != nil {
			t.Fatalf("报名信息整表替换失败: %v", err)
		}
		// 二次替换：MySQL 上「先删后插同事务」与唯一索引 race_id 的交互和 SQLite 不完全一样
		if _, err := ReplaceRaceRegistrations(db.Gorm(), rows); err != nil {
			t.Fatalf("报名信息二次替换失败: %v", err)
		}
		got, err := RaceRegistrationsByRaceIDs(db.Gorm(), []string{"1", "2", "不存在"})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got["1"].MinPrice != 170 {
			t.Errorf("批量取不对: %+v", got)
		}
		// 空入参不能拼出 `IN ()`
		if empty, err := RaceRegistrationsByRaceIDs(db.Gorm(), nil); err != nil || len(empty) != 0 {
			t.Errorf("空入参应返回空 map: %v %v", empty, err)
		}
		if _, err := ReplaceRaceRegistrations(db.Gorm(), nil); err != nil {
			t.Fatalf("清空失败: %v", err)
		}
		if n, _ := CountRaceRegistrations(db.Gorm()); n != 0 {
			t.Errorf("清空后还剩 %d 条", n)
		}
	})
}
