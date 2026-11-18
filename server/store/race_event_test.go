package store

import (
	"path/filepath"
	"testing"

	"github.com/fghosth/openrunner/core/races"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "race-test.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func raceRow(id, name, date, prov, city, dist, cats string) RaceEventModel {
	year := 0
	if len(date) >= 4 {
		year = int(date[0]-'0')*1000 + int(date[1]-'0')*100 + int(date[2]-'0')*10 + int(date[3]-'0')
	}
	return RaceEventModel{
		RaceID: id, Name: name, Date: date, Year: year, Grade: "A",
		Province: prov, City: city, District: dist, Categories: cats,
	}
}

func TestReplaceRaceEventsIsAtomic(t *testing.T) {
	db := openTestDB(t).Gorm()

	rows := []RaceEventModel{
		raceRow("1", "甲马", "2026-03-01", "广东省", "广州市", "天河区", "full,half"),
		raceRow("2", "乙马", "2026-04-01", "广东省", "深圳市", "", "half"),
		raceRow("3", "丙马", "2026-05-01", "江苏省", "南京市", "玄武区", "10k"),
	}
	n, err := ReplaceRaceEvents(db, rows)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("应写入 3 条，实际 %d", n)
	}
	if c, _ := CountRaceEvents(db); c != 3 {
		t.Errorf("计数不对: %d", c)
	}

	// 整表替换：旧行必须消失，不能增量合并出幽灵
	if _, err := ReplaceRaceEvents(db, rows[:1]); err != nil {
		t.Fatal(err)
	}
	if c, _ := CountRaceEvents(db); c != 1 {
		t.Errorf("替换后应只剩 1 条，实际 %d", c)
	}

	// 重复 race_id 会让第二批插入失败，事务必须整体回滚（不能留半张表）
	before, _ := CountRaceEvents(db)
	dup := []RaceEventModel{raceRow("9", "新马", "2026-06-01", "浙江省", "杭州市", "", "full")}
	if _, err := ReplaceRaceEvents(db, append([]RaceEventModel{raceRow("9", "撞号", "2026-06-02", "浙江省", "温州市", "", "half")}, dup...)); err == nil {
		t.Log("上游若出现重复 race_id 不会报错 —— 检查唯一索引是否生效")
	}
	after, _ := CountRaceEvents(db)
	if after != before {
		t.Errorf("失败的那次替换不该留下半张表：前 %d 后 %d", before, after)
	}
}

func TestRaceSummaryTree(t *testing.T) {
	db := openTestDB(t).Gorm()
	if _, err := ReplaceRaceEvents(db, []RaceEventModel{
		raceRow("1", "A", "2026-03-01", "广东省", "广州市", "天河区", "full,half"),
		raceRow("2", "B", "2026-03-08", "广东省", "广州市", "越秀区", "half"),
		raceRow("3", "C", "2026-04-01", "广东省", "深圳市", "", "half"),
		raceRow("4", "D", "2025-11-01", "江苏省", "南京市", "玄武区", "10k"),
	}); err != nil {
		t.Fatal(err)
	}

	sum, err := RaceSummary(db, RaceFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Total != 4 {
		t.Fatalf("总数不对: %d", sum.Total)
	}
	if len(sum.Provinces) != 2 || sum.Provinces[0].Name != "广东省" || sum.Provinces[0].Count != 3 {
		t.Errorf("省排序/计数不对: %+v", sum.Provinces)
	}
	gd := sum.Provinces[0]
	if len(gd.Cities) != 2 || gd.Cities[0].Name != "广州市" || gd.Cities[0].Count != 2 {
		t.Errorf("市不对: %+v", gd.Cities)
	}
	if len(gd.Cities[0].Districts) != 2 {
		t.Errorf("区应有 2 个: %+v", gd.Cities[0].Districts)
	}
	// 深圳市没有区信息：市计数仍为 1，但不该冒出空的区条目
	sz := gd.Cities[1]
	if sz.Name != "深圳市" || sz.Count != 1 || len(sz.Districts) != 0 {
		t.Errorf("无区的市不该有假区条目: %+v", sz)
	}
	// 年份倒序，供 UI 直接做下拉
	if len(sum.Years) != 2 || sum.Years[0] != 2026 || sum.Years[1] != 2025 {
		t.Errorf("年份列表不对: %v", sum.Years)
	}
}

func TestRaceSummaryEmptyTable(t *testing.T) {
	db := openTestDB(t).Gorm()
	sum, err := RaceSummary(db, RaceFilter{})
	if err != nil {
		t.Fatalf("空表不该报错: %v", err)
	}
	if sum.Total != 0 || len(sum.Provinces) != 0 {
		t.Errorf("空表应给空树，实际 %+v", sum)
	}
}

func TestRaceFilters(t *testing.T) {
	db := openTestDB(t).Gorm()
	if _, err := ReplaceRaceEvents(db, []RaceEventModel{
		raceRow("1", "广州马拉松", "2026-12-01", "广东省", "广州市", "天河区", "full,half"),
		raceRow("2", "深圳半马", "2026-11-01", "广东省", "深圳市", "", "half"),
		raceRow("3", "南京 10K", "2025-10-01", "江苏省", "南京市", "", "10k"),
	}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		f     RaceFilter
		want  int
		first string
	}{
		{"按省", RaceFilter{Province: "广东省"}, 2, "广州马拉松"},
		{"按市", RaceFilter{Province: "广东省", City: "广州市"}, 1, "广州马拉松"},
		{"按年份", RaceFilter{Year: 2025}, 1, "南京 10K"},
		{"按项目", RaceFilter{Category: "10k"}, 1, "南京 10K"},
		{"按项目 half", RaceFilter{Category: "half"}, 2, "广州马拉松"},
		{"按名称", RaceFilter{Name: "广州"}, 1, "广州马拉松"},
		// 不认识的 category 必须被忽略，而不是变成 LIKE '%%' 把全部捞出来
		{"非法项目被忽略", RaceFilter{Category: "ultra"}, 3, "广州马拉松"},
	}
	for _, tc := range cases {
		got, err := ListRaceEvents(db, tc.f)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if len(got) != tc.want {
			t.Errorf("%s：期望 %d 条，实际 %d %+v", tc.name, tc.want, len(got), got)
			continue
		}
		if tc.want > 0 && got[0].Name != tc.first {
			t.Errorf("%s：首条应为 %s，实际 %s", tc.name, tc.first, got[0].Name)
		}
	}
}

func TestRaceListLimitDoesNotTouchSummary(t *testing.T) {
	db := openTestDB(t).Gorm()
	if _, err := ReplaceRaceEvents(db, []RaceEventModel{
		raceRow("1", "广州马拉松", "2026-12-01", "广东省", "广州市", "天河区", "full"),
		raceRow("2", "深圳半马", "2026-11-01", "广东省", "深圳市", "", "half"),
		raceRow("3", "南京 10K", "2025-10-01", "江苏省", "南京市", "", "10k"),
	}); err != nil {
		t.Fatal(err)
	}

	list, err := ListRaceEvents(db, RaceFilter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("Limit=2 应只取 2 条，实际 %d", len(list))
	}
	// 日期倒序 + limit = 「最近的 2 场」，不是随机 2 场
	if list[0].Date != "2026-12-01" || list[1].Date != "2026-11-01" {
		t.Errorf("取到的不是最近的 2 场: %+v", list)
	}

	// 关键：limit 绝不能渗进 summary，否则省/市/区计数会被截断
	sum, err := RaceSummary(db, RaceFilter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Total != 3 || len(sum.Provinces) != 2 {
		t.Errorf("summary 不该受 Limit 影响，实际 total=%d provinces=%d", sum.Total, len(sum.Provinces))
	}
}

func TestRaceEventCategoriesRoundTrip(t *testing.T) {
	m := raceRow("1", "甲马", "2026-03-01", "广东省", "广州市", "", "full,half")
	if !m.Has(races.CategoryFull) || !m.Has(races.CategoryHalf) || m.Has(races.CategoryTen) {
		t.Errorf("项目回读不对: %+v", m.CategoriesList())
	}
}

func TestRaceCalendarMeta(t *testing.T) {
	db := openTestDB(t).Gorm()
	m, err := GetRaceCalendarMeta(db)
	if err != nil {
		t.Fatalf("没有记录时不该报错: %v", err)
	}
	if m.LastSyncAt != 0 {
		t.Errorf("新装实例应是零值: %+v", m)
	}
	if err := SaveRaceCalendarMeta(db, 1700000000, 2956, ""); err != nil {
		t.Fatal(err)
	}
	if err := SaveRaceCalendarMeta(db, 1700003600, 2960, "上游超时"); err != nil {
		t.Fatal(err)
	}
	m, err = GetRaceCalendarMeta(db)
	if err != nil {
		t.Fatal(err)
	}
	if m.LastSyncAt != 1700003600 || m.Total != 2960 || m.LastError != "上游超时" {
		t.Errorf("meta 更新不对: %+v", m)
	}
}

func TestRaceGradeFilterAndUnlabeled(t *testing.T) {
	db := openTestDB(t).Gorm()
	rows := []RaceEventModel{
		raceRow("1", "A 级马", "2026-12-01", "广东省", "广州市", "", "full"),
		raceRow("2", "B 级马", "2026-11-01", "广东省", "深圳市", "", "half"),
		raceRow("3", "没标级别", "2026-10-01", "广东省", "珠海市", "", "half"),
	}
	rows[1].Grade = "B"
	rows[2].Grade = "" // 上游有 34 条没给级别
	if _, err := ReplaceRaceEvents(db, rows); err != nil {
		t.Fatal(err)
	}

	got, err := ListRaceEvents(db, RaceFilter{Grade: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "B 级马" {
		t.Fatalf("按级别筛不对: %+v", got)
	}

	sum, err := RaceSummary(db, RaceFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Grades) != 3 {
		t.Fatalf("级别列表应有 3 档（含未标注）: %+v", sum.Grades)
	}
	if sum.Grades[len(sum.Grades)-1].Name != unlabeled {
		t.Errorf("未标注应垫底，实际 %+v", sum.Grades)
	}

	// 占位名必须能翻回空串：否则树上点得到的「未标注」，列表永远空
	got, err = ListRaceEvents(db, RaceFilter{Grade: unlabeled})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "没标级别" {
		t.Errorf("按「%s」筛应命中空级别那条，实际 %+v", unlabeled, got)
	}
}
