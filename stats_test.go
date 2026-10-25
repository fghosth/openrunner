package core

import "testing"

// 年度统计恒等式（票 11）。纯函数：给定日级活动摘要，算年/月/连续天数。

func mkDays(dates ...string) []DayActivity {
	out := make([]DayActivity, 0, len(dates))
	for _, d := range dates {
		out = append(out, DayActivity{Date: d, DistanceM: 5000, DurationSecs: 1800, Count: 1})
	}
	return out
}

func TestAnnualStats(t *testing.T) {
	t.Run("全年汇总", func(t *testing.T) {
		days := []DayActivity{
			{Date: "2026-01-01", DistanceM: 10000, DurationSecs: 3600, Count: 1},
			{Date: "2026-01-02", DistanceM: 5000, DurationSecs: 1800, Count: 2}, // 同日两次
			{Date: "2026-03-15", DistanceM: 21097, DurationSecs: 7200, Count: 1},
			{Date: "2025-12-31", DistanceM: 9999, DurationSecs: 3000, Count: 1}, // 上一年，应排除
		}
		s := AnnualStats(days, 2026)
		if s.Year != 2026 {
			t.Fatalf("期望 year=2026, 得到 %d", s.Year)
		}
		if s.TotalDistanceM != 36097 {
			t.Errorf("期望距离 36097, 得到 %v", s.TotalDistanceM)
		}
		if s.TotalDurationSecs != 12600 {
			t.Errorf("期望时长 12600, 得到 %v", s.TotalDurationSecs)
		}
		if s.ActivityCount != 4 {
			t.Errorf("期望 4 次（含同日两次）, 得到 %d", s.ActivityCount)
		}
		if s.ActiveDays != 3 {
			t.Errorf("期望 3 个活动日, 得到 %d", s.ActiveDays)
		}
	})

	t.Run("按月汇总", func(t *testing.T) {
		days := []DayActivity{
			{Date: "2026-01-10", DistanceM: 1000, Count: 1},
			{Date: "2026-01-20", DistanceM: 2000, Count: 1},
			{Date: "2026-02-05", DistanceM: 3000, Count: 1},
		}
		s := AnnualStats(days, 2026)
		if len(s.Monthly) != 12 {
			t.Fatalf("期望 12 个月桶, 得到 %d", len(s.Monthly))
		}
		if s.Monthly[0].DistanceM != 3000 || s.Monthly[0].Count != 2 {
			t.Errorf("期望 1 月 3000m/2 次, 得到 %+v", s.Monthly[0])
		}
		if s.Monthly[1].DistanceM != 3000 || s.Monthly[1].Count != 1 {
			t.Errorf("期望 2 月 3000m/1 次, 得到 %+v", s.Monthly[1])
		}
		if s.Monthly[2].DistanceM != 0 {
			t.Errorf("期望 3 月空, 得到 %+v", s.Monthly[2])
		}
	})

	t.Run("空年份不 panic", func(t *testing.T) {
		s := AnnualStats(nil, 2026)
		if s.ActivityCount != 0 || s.TotalDistanceM != 0 || len(s.Monthly) != 12 {
			t.Fatalf("期望空统计, 得到 %+v", s)
		}
	})
}

func TestStreaks(t *testing.T) {
	t.Run("最长连续与当前连续", func(t *testing.T) {
		// 1/1-1/5 连续 5 天，1/8-1/9 连续 2 天，1/12 单天
		days := mkDays(
			"2026-01-01", "2026-01-02", "2026-01-03", "2026-01-04", "2026-01-05",
			"2026-01-08", "2026-01-09",
			"2026-01-12",
		)
		s := Streaks(days)
		if s.Longest != 5 {
			t.Errorf("期望最长 5 天, 得到 %d", s.Longest)
		}
		if s.Current != 1 {
			t.Errorf("期望当前 1 天（1/12 单天）, 得到 %d", s.Current)
		}
	})

	t.Run("跨月连续", func(t *testing.T) {
		s := Streaks(mkDays("2026-01-30", "2026-01-31", "2026-02-01"))
		if s.Longest != 3 {
			t.Errorf("期望跨月连续 3 天, 得到 %d", s.Longest)
		}
	})

	t.Run("同日多次不重复计数", func(t *testing.T) {
		days := []DayActivity{
			{Date: "2026-01-01", Count: 2},
			{Date: "2026-01-01", Count: 1},
			{Date: "2026-01-02", Count: 1},
		}
		if s := Streaks(days); s.Longest != 2 {
			t.Errorf("期望 2（同日合并）, 得到 %d", s.Longest)
		}
	})

	t.Run("无活动 → 0", func(t *testing.T) {
		if s := Streaks(nil); s.Longest != 0 || s.Current != 0 {
			t.Fatalf("期望 0, 得到 %+v", s)
		}
	})
}

func TestDownsampleTrack(t *testing.T) {
	t.Run("点数不足上限 → 原样返回", func(t *testing.T) {
		lats := []float64{31.0, 31.1, 31.2}
		lons := []float64{121.0, 121.1, 121.2}
		pts := DownsampleTrack(lats, lons, 10)
		if len(pts) != 3 || pts[2][0] != 31.2 {
			t.Fatalf("期望 3 点原样, 得到 %v", pts)
		}
	})

	t.Run("超限按等距抽稀且保留首尾", func(t *testing.T) {
		n := 1000
		lats := make([]float64, n)
		lons := make([]float64, n)
		for i := 0; i < n; i++ {
			lats[i] = 31.0 + float64(i)*0.001
			lons[i] = 121.0 + float64(i)*0.001
		}
		pts := DownsampleTrack(lats, lons, 100)
		if len(pts) != 100 {
			t.Fatalf("期望 100 点, 得到 %d", len(pts))
		}
		if pts[0][0] != 31.0 {
			t.Errorf("期望保留首点 31.0, 得到 %v", pts[0][0])
		}
		if pts[len(pts)-1][0] != lats[n-1] {
			t.Errorf("期望保留末点 %v, 得到 %v", lats[n-1], pts[len(pts)-1][0])
		}
	})

	t.Run("跳过无效点（0,0 与越界）", func(t *testing.T) {
		lats := []float64{31.0, 0, 31.2, 999}
		lons := []float64{121.0, 0, 121.2, 999}
		pts := DownsampleTrack(lats, lons, 100)
		if len(pts) != 2 {
			t.Fatalf("期望只保留 2 个有效点, 得到 %d: %v", len(pts), pts)
		}
	})
}
