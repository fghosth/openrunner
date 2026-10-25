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

func TestIsValidCoordinate(t *testing.T) {
	cases := []struct {
		lat, lon float64
		want     bool
	}{
		{31.23, 121.47, true},
		{0, 0, false},          // 缺测
		{91, 0, false},         // 纬度越界
		{0, 181, false},        // 经度越界
		{-33.9, 151.2, true},   // 南半球
	}
	for _, c := range cases {
		if got := IsValidCoordinate(c.lat, c.lon); got != c.want {
			t.Fatalf("IsValidCoordinate(%v,%v) 期望 %v 得到 %v", c.lat, c.lon, c.want, got)
		}
	}
}

func TestFastestForDistance(t *testing.T) {
	t.Run("匀速 12km/h 跑 1km 应约 300 秒", func(t *testing.T) {
		speeds := make([]float64, 900) // 900 秒
		for i := range speeds {
			speeds[i] = 12 // km/h → 3.333 m/s
		}
		secs, ok := FastestForDistance(speeds, 1, 1000)
		if !ok {
			t.Fatal("期望找到结果")
		}
		// 1000m / 3.3333 m/s = 300s
		if secs < 299 || secs > 301 {
			t.Fatalf("期望约 300 秒, 得到 %v", secs)
		}
	})

	t.Run("取活动内部最快分段而非整条活动", func(t *testing.T) {
		// 前 200 秒慢速 8km/h，随后 200 秒快速 16km/h
		speeds := make([]float64, 400)
		for i := 0; i < 200; i++ {
			speeds[i] = 8
		}
		for i := 200; i < 400; i++ {
			speeds[i] = 16
		}
		secs, ok := FastestForDistance(speeds, 1, 800)
		if !ok {
			t.Fatal("期望找到结果")
		}
		// 16km/h = 4.444 m/s → 800m 需 180s（全在快段内）
		if secs > 185 {
			t.Fatalf("期望约 180 秒（快段内），得到 %v —— 说明没取到内部最快分段", secs)
		}
	})

	t.Run("活动长度不足", func(t *testing.T) {
		speeds := []float64{10, 10, 10} // 只有 ~8m
		if _, ok := FastestForDistance(speeds, 1, 5000); ok {
			t.Fatal("距离不足时应返回 false")
		}
	})

	t.Run("含停顿采样点", func(t *testing.T) {
		// 13 个点、其中 2 个 0：总距离约 36.7m，目标 30m 需要跨过停顿点
		speeds := []float64{12, 0, 12, 0, 12, 12, 12, 12, 12, 12, 12, 12, 12}
		secs, ok := FastestForDistance(speeds, 1, 30)
		if !ok || secs <= 0 {
			t.Fatalf("应能跨过停顿点算出结果, 得到 %v %v", secs, ok)
		}
		// 3.3333 m/s → 30m 需 9 个采样秒；含 2 个 0 的窗口会更长，但不应超过全部 13 秒
		if secs > 13 {
			t.Fatalf("用时不应超过整个活动长度, 得到 %v", secs)
		}
	})
}

func TestFastestOverDistance(t *testing.T) {
	t.Run("匀速 4:10/km 跑 1km", func(t *testing.T) {
		// 每 10 秒 100 米（10 m/s）→ 1000m 需 100 秒
		cum := make([]float64, 0, 601)
		for i := 0; i <= 600; i++ {
			cum = append(cum, float64(i)*100)
		}
		secs, ok := FastestOverDistance(cum, 10, 1000)
		if !ok {
			t.Fatal("应找到结果")
		}
		if secs != 100 {
			t.Fatalf("期望 100 秒, 得到 %v", secs)
		}
	})

	t.Run("边界情况：累计距离刚够而速度积分不足", func(t *testing.T) {
		// 模拟真实事故：距离流末值 10015m（够 10km），速度积分只有 9991m
		cum := []float64{0, 5000, 10015}
		secs, ok := FastestOverDistance(cum, 1, 10000)
		if !ok {
			t.Fatal("距离流够长时必须能算出结果（这正是修复的 bug）")
		}
		if secs <= 0 {
			t.Fatalf("用时必须为正, 得到 %v", secs)
		}
	})

	t.Run("距离不足返回 false", func(t *testing.T) {
		if _, ok := FastestOverDistance([]float64{0, 100, 200}, 1, 1000); ok {
			t.Fatal("距离不足应返回 false")
		}
	})
}
