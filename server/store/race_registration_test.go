package store

import "testing"

// 报名信息的存储测试。
//
// 这张表的存在理由就是「race_events 每次同步整表替换，所以报名信息必须外挂」，
// 所以测试重点全在替换语义上：替换要真的换掉，清空要真的清空。

func TestReplaceRaceRegistrationsReplaces(t *testing.T) {
	db := openTestDB(t).Gorm()
	rows := []RaceRegistrationModel{
		{RaceID: "r1", Source: "zuicool", SourceID: "84480", RegEnd: "2026-11-18T12:00:00", MinPrice: 170},
		{RaceID: "r2", Source: "zuicool", SourceID: "90001", RegStart: "2026-11-22T18:00:00"},
	}
	n, err := ReplaceRaceRegistrations(db, rows)
	if err != nil {
		t.Fatalf("首次替换: %v", err)
	}
	if n != 2 {
		t.Fatalf("写入 %d 条，应为 2", n)
	}
	// 二次替换必须换掉而不是叠加 —— race_id 上有唯一索引，叠加会直接失败
	if _, err := ReplaceRaceRegistrations(db, rows); err != nil {
		t.Fatalf("二次替换: %v", err)
	}
	if got, _ := CountRaceRegistrations(db); got != 2 {
		t.Errorf("二次替换后剩 %d 条，应为 2（没清旧就会撞唯一索引）", got)
	}
	// 下架的比赛要跟着消失，否则会一直显示一个已经关掉的截止日期
	if _, err := ReplaceRaceRegistrations(db, rows[:1]); err != nil {
		t.Fatalf("缩表替换: %v", err)
	}
	got, err := RaceRegistrationsByRaceIDs(db, []string{"r1", "r2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["r1"].MinPrice != 170 {
		t.Errorf("缩表后不对： %+v", got)
	}
	if _, still := got["r2"]; still {
		t.Error("已下架的报名没被清掉")
	}
}

func TestRaceRegistrationsEmptyIDs(t *testing.T) {
	db := openTestDB(t).Gorm()
	// 空入参必须短路 —— 拼出 `IN ()` 是非法 SQL
	got, err := RaceRegistrationsByRaceIDs(db, nil)
	if err != nil {
		t.Fatalf("空入参不该报错: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("空入参应返回空 map，实际 %v", got)
	}
}

func TestRaceRegistrationsRoundTripFields(t *testing.T) {
	db := openTestDB(t).Gorm()
	want := RaceRegistrationModel{
		RaceID: "r9", Source: "zuicool", SourceID: "123",
		RegStart: "2026-10-28T10:00:00", RegEnd: "2026-11-18T12:00:00",
		MinPrice: 169.5, RegURL: "https://reg.zuicool.com/34604/233355", FetchedAt: 1794881181,
	}
	if _, err := ReplaceRaceRegistrations(db, []RaceRegistrationModel{want}); err != nil {
		t.Fatal(err)
	}
	got, err := RaceRegistrationsByRaceIDs(db, []string{"r9"})
	if err != nil {
		t.Fatal(err)
	}
	g := got["r9"]
	if g.RegStart != want.RegStart || g.RegEnd != want.RegEnd ||
		g.MinPrice != want.MinPrice || g.RegURL != want.RegURL || g.FetchedAt != want.FetchedAt {
		t.Errorf("字段回读不一致：\n got %+v\nwant %+v", g, want)
	}
}

// 旧年份自愈：keepYear 只过滤新拉的数据，存量残留得靠这两个函数清。
func TestDeleteRaceEventsBeforeYear(t *testing.T) {
	db := openTestDB(t).Gorm()
	if _, err := ReplaceRaceEvents(db, []RaceEventModel{
		raceRow("1", "旧赛 A", "2024-05-01", "江苏省", "南京市", "", "full"),
		raceRow("2", "旧赛 B", "2025-10-01", "江苏省", "苏州市", "", "half"),
		raceRow("3", "当年赛", "2026-12-01", "江苏省", "无锡市", "", "full"),
	}); err != nil {
		t.Fatal(err)
	}
	n, err := CountRaceEventsBeforeYear(db, 2026)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("数出 %d 行旧数据，应为 2", n)
	}
	del, err := DeleteRaceEventsBeforeYear(db, 2026)
	if err != nil {
		t.Fatal(err)
	}
	if del != 2 {
		t.Errorf("删了 %d 行，应为 2", del)
	}
	// 幂等：再跑一次应该是 0，不会把当年的也带走
	if del2, _ := DeleteRaceEventsBeforeYear(db, 2026); del2 != 0 {
		t.Errorf("二次执行删了 %d 行，应为 0", del2)
	}
	left, _ := CountRaceEvents(db)
	if left != 1 {
		t.Errorf("剩 %d 行，应为 1（只该留下 2026 的）", left)
	}
}
