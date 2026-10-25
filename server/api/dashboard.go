package api

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/openrunner/core"
	"github.com/openrunner/server/store"
	"gorm.io/gorm"
)

// periodTotals 一个时间段的汇总（年/月/周/全部）。
type periodTotals struct {
	DistanceM      float64 `json:"distanceM"`
	Count          int     `json:"count"`
	DurationSecs   int     `json:"durationSecs"`
	ElevationGainM float64 `json:"elevationGainM"`
}

// sportTotals 按运动类型聚合（用于「跑步/骑行/其他」分布）。
type sportTotals struct {
	Sport     string  `json:"sport"`
	DistanceM float64 `json:"distanceM"`
	Count     int     `json:"count"`
}

// dashboardDTO 是首页需要的全部轻量数据（一次请求，避免前端拼多次）。
type dashboardDTO struct {
	Year   int                `json:"year"`
	Years  []int              `json:"years"`
	Daily  []core.DayActivity `json:"daily"` // 选定年份的逐日摘要（热力图数据源）
	Totals struct {
		All   periodTotals `json:"all"`
		Year  periodTotals `json:"year"`
		Month periodTotals `json:"month"`
		Week  periodTotals `json:"week"`
	} `json:"totals"`
	BySport []sportTotals `json:"bySport"`
}

// handleDashboard 首页聚合：逐年逐日摘要 + 年/月/周合计 + 运动类型分布。
//
// GET /api/athletes/{athleteId}/dashboard?year=2026
func handleDashboard(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		if ok, err := store.AthleteExists(db, uint(athleteID)); err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		} else if !ok {
			writeError(w, http.StatusNotFound, "Athlete 不存在")
			return
		}

		// 只统计实际活动（计划活动不计入「今年跑了多少」）
		var rows []struct {
			ID             uint
			StartTime      int64
			Sport          string
			DistanceM      float64
			DurationSecs   int
			ElevationGainM float64
		}
		if err := db.Table("activities").
			Select("id, start_time, sport, distance_m, duration_secs, elevation_gain_m").
			Where("athlete_id = ? AND is_planned = ?", uint(athleteID), false).
			Scan(&rows).Error; err != nil {
			writeError(w, http.StatusInternalServerError, "聚合失败")
			return
		}

		year := time.Now().Local().Year()
		if q := r.URL.Query().Get("year"); q != "" {
			if v, err := strconv.Atoi(q); err == nil && v >= 1900 && v <= 2200 {
				year = v
			}
		}

		out := dashboardDTO{Year: year}
		out.Daily = []core.DayActivity{}
		out.BySport = []sportTotals{}

		yearSet := map[int]bool{}
		dayMap := map[string]*core.DayActivity{}
		dayOrder := []string{}
		sportMap := map[string]*sportTotals{}

		now := time.Now().Local()
		monthPrefix := now.Format("2006-01")
		// 本周起点：周一（ISO 周）
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		weekStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).
			AddDate(0, 0, -(weekday - 1))
		weekStartUnix := weekStart.Unix()

		for _, row := range rows {
			t := time.Unix(row.StartTime, 0).Local()
			date := t.Format("2006-01-02")
			y := t.Year()
			yearSet[y] = true

			add := func(p *periodTotals) {
				p.DistanceM += row.DistanceM
				p.Count++
				p.DurationSecs += row.DurationSecs
				p.ElevationGainM += row.ElevationGainM
			}
			add(&out.Totals.All)
			if y == year {
				add(&out.Totals.Year)
				if d, ok := dayMap[date]; ok {
					d.DistanceM += row.DistanceM
					d.DurationSecs += row.DurationSecs
					d.Count++
				} else {
					dayMap[date] = &core.DayActivity{
						Date: date, DistanceM: row.DistanceM,
						DurationSecs: row.DurationSecs, Count: 1,
					}
					dayOrder = append(dayOrder, date)
				}
				if s, ok := sportMap[row.Sport]; ok {
					s.DistanceM += row.DistanceM
					s.Count++
				} else {
					sportMap[row.Sport] = &sportTotals{
						Sport: row.Sport, DistanceM: row.DistanceM, Count: 1,
					}
				}
			}
			// 月/周进度只在看当前年时才有意义
			if y == now.Year() {
				if date[:7] == monthPrefix {
					add(&out.Totals.Month)
				}
				if row.StartTime >= weekStartUnix {
					add(&out.Totals.Week)
				}
			}
		}

		sort.Strings(dayOrder)
		for _, d := range dayOrder {
			out.Daily = append(out.Daily, *dayMap[d])
		}
		for _, y := range sortedYears(yearSet) {
			out.Years = append(out.Years, y)
		}
		for _, s := range sportMap {
			out.BySport = append(out.BySport, *s)
		}
		sort.Slice(out.BySport, func(i, j int) bool {
			return out.BySport[i].DistanceM > out.BySport[j].DistanceM
		})

		writeJSON(w, http.StatusOK, out)
	}
}

func sortedYears(set map[int]bool) []int {
	out := make([]int, 0, len(set))
	for y := range set {
		out = append(out, y)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	return out
}

// pbTarget 是一个个人最佳的目标距离。
type pbTarget struct {
	Label     string  `json:"label"`
	DistanceM float64 `json:"distanceM"`
}

// pbResult 是某个距离的最快成绩。
type pbResult struct {
	pbTarget
	Seconds       float64 `json:"seconds"`
	PaceSecsPerKm float64 `json:"paceSecsPerKm"`
	ActivityID    uint    `json:"activityId"`
	ActivityStart int64   `json:"activityStart"`
}

var pbTargets = []pbTarget{
	{Label: "1K", DistanceM: 1000},
	{Label: "5K", DistanceM: 5000},
	{Label: "10K", DistanceM: 10000},
	{Label: "半马", DistanceM: 21097.5},
	{Label: "全马", DistanceM: 42195},
}

// handlePersonalBest 扫描跑步活动的速度流，取各目标距离的**最快分段**。
//
// 与 running_page 的做法不同：那个用「总距离落在 4.8~5.5km 的活动取总时长」，
// 只能找到整条活动恰好是 5K 的情况。这里在每条活动内部找最短窗口覆盖目标距离，
// 因此一次 10K 跑里的前半段也能成为 5K 的个人最佳。
//
// GET /api/athletes/{athleteId}/personal-best
func handlePersonalBest(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		if ok, err := store.AthleteExists(db, uint(athleteID)); err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		} else if !ok {
			writeError(w, http.StatusNotFound, "Athlete 不存在")
			return
		}

		var acts []store.ActivityModel
		if err := db.Where("athlete_id = ? AND sport = ? AND has_stream = ?",
			uint(athleteID), string(core.SportRun), true).
			Order("start_time ASC").Find(&acts).Error; err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}

		best := make([]pbResult, len(pbTargets))
		for i, t := range pbTargets {
			best[i] = pbResult{pbTarget: t}
		}

		for i := range acts {
			a := &acts[i]
			sm, err := store.GetStream(db, a.ID)
			if err != nil {
				continue
			}
			stream, err := core.UnmarshalStream(sm.Data)
			if err != nil || stream.RecIntervalSecs <= 0 || len(stream.SpeedKph) == 0 {
				continue
			}
			for j, t := range pbTargets {
				secs, ok := core.FastestForDistance(stream.SpeedKph, stream.RecIntervalSecs, t.DistanceM)
				if !ok || secs <= 0 {
					continue
				}
				if best[j].Seconds == 0 || secs < best[j].Seconds {
					best[j].Seconds = secs
					best[j].PaceSecsPerKm = secs / (t.DistanceM / 1000)
					best[j].ActivityID = a.ID
					best[j].ActivityStart = a.StartTime
				}
			}
		}

		// 只返回真正找到成绩的项
		out := make([]pbResult, 0, len(best))
		for _, b := range best {
			if b.Seconds > 0 {
				out = append(out, b)
			}
		}
		writeJSON(w, http.StatusOK, out)
	}
}
