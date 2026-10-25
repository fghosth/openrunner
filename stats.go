package core

import (
	"sort"
	"time"
)

// 年度统计与轨迹抽稀（票 11）。纯函数，供 server 聚合后再加工。

// DayActivity 日级活动摘要（server 从数据库聚合后传入）。
type DayActivity struct {
	Date         string  `json:"date"` // YYYY-MM-DD
	DistanceM    float64 `json:"distanceM"`
	DurationSecs int     `json:"durationSecs"`
	Count        int     `json:"count"` // 当日活动次数
}

// MonthStat 月汇总。
type MonthStat struct {
	Month        int     `json:"month"` // 1-12
	DistanceM    float64 `json:"distanceM"`
	DurationSecs int     `json:"durationSecs"`
	Count        int     `json:"count"`
	ActiveDays   int     `json:"activeDays"`
}

// AnnualStat 年度统计。
type AnnualStat struct {
	Year              int         `json:"year"`
	TotalDistanceM    float64     `json:"totalDistanceM"`
	TotalDurationSecs int         `json:"totalDurationSecs"`
	ActivityCount     int         `json:"activityCount"`
	ActiveDays        int         `json:"activeDays"`
	Monthly           []MonthStat `json:"monthly"`
}

// AnnualStats 汇总指定年份（按 Date 前缀过滤）。
func AnnualStats(days []DayActivity, year int) AnnualStat {
	stat := AnnualStat{Year: year, Monthly: make([]MonthStat, 12)}
	for i := range stat.Monthly {
		stat.Monthly[i].Month = i + 1
	}
	prefix := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC).Format("2006")
	for _, d := range days {
		if len(d.Date) < 4 || d.Date[:4] != prefix {
			continue
		}
		stat.TotalDistanceM += d.DistanceM
		stat.TotalDurationSecs += d.DurationSecs
		stat.ActivityCount += d.Count
		if d.Count > 0 {
			stat.ActiveDays++
		}
		month := 0
		if len(d.Date) >= 7 {
			if t, err := time.Parse("2006-01-02", d.Date); err == nil {
				month = int(t.Month())
			}
		}
		if month >= 1 && month <= 12 {
			m := &stat.Monthly[month-1]
			m.DistanceM += d.DistanceM
			m.DurationSecs += d.DurationSecs
			m.Count += d.Count
			if d.Count > 0 {
				m.ActiveDays++
			}
		}
	}
	return stat
}

// StreakStat 连续活动天数。
type StreakStat struct {
	Longest int `json:"longest"` // 历史最长连续天数
	Current int `json:"current"` // 至今（最后一个活动日向前）连续天数
}

// Streaks 计算最长/当前连续活动天数（同日多次合并为 1 天）。
func Streaks(days []DayActivity) StreakStat {
	active := map[string]bool{}
	for _, d := range days {
		if d.Count > 0 && d.Date != "" {
			active[d.Date] = true
		}
	}
	if len(active) == 0 {
		return StreakStat{}
	}
	dates := make([]string, 0, len(active))
	for d := range active {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	longest, run := 1, 1
	for i := 1; i < len(dates); i++ {
		if consecutiveDay(dates[i-1], dates[i]) {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 1
		}
	}

	// 当前连续：从最后一个活动日往前数
	current := 1
	for i := len(dates) - 1; i > 0; i-- {
		if consecutiveDay(dates[i-1], dates[i]) {
			current++
		} else {
			break
		}
	}
	return StreakStat{Longest: longest, Current: current}
}

func consecutiveDay(prev, next string) bool {
	p, err1 := time.Parse("2006-01-02", prev)
	n, err2 := time.Parse("2006-01-02", next)
	if err1 != nil || err2 != nil {
		return false
	}
	return n.Sub(p) == 24*time.Hour
}

// DownsampleTrack 轨迹抽稀为 [lat, lon] 点对，最多 maxPoints 个，保留首尾。
// 跳过无效坐标（0/0 缺测、越界）——地图上不画这些点。
func DownsampleTrack(lats, lons []float64, maxPoints int) [][2]float64 {
	if maxPoints <= 0 {
		return nil
	}
	valid := make([][2]float64, 0, len(lats))
	n := len(lats)
	if len(lons) < n {
		n = len(lons)
	}
	for i := 0; i < n; i++ {
		lat, lon := lats[i], lons[i]
		if lat == 0 && lon == 0 {
			continue
		}
		if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
			continue
		}
		valid = append(valid, [2]float64{lat, lon})
	}
	if len(valid) <= maxPoints {
		return valid
	}
	// 等距抽稀：步长 len/(maxPoints-1)，保留首尾
	out := make([][2]float64, 0, maxPoints)
	step := float64(len(valid)-1) / float64(maxPoints-1)
	for i := 0; i < maxPoints-1; i++ {
		out = append(out, valid[int(float64(i)*step)])
	}
	out = append(out, valid[len(valid)-1])
	return out
}
