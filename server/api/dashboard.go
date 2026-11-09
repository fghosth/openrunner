package api

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/fghosth/openrunner/core"
	"github.com/fghosth/openrunner/server/store"
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

// dayDTO 是逐日聚合；按运动分列距离，供热力图按类型配色。
type dayDTO struct {
	Date         string  `json:"date"`
	DistanceM    float64 `json:"distanceM"`
	DurationSecs int     `json:"durationSecs"`
	Count        int     `json:"count"`
	RunMeters    float64 `json:"runMeters"`
	RideMeters   float64 `json:"rideMeters"`
}

// dashboardDTO 是首页需要的全部轻量数据（一次请求，避免前端拼多次）。
type dashboardDTO struct {
	Year   int      `json:"year"`
	Years  []int    `json:"years"`
	Daily  []dayDTO `json:"daily"` // **全部年份**的逐日摘要（热力图按年分组用）
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
		out.Daily = []dayDTO{}
		out.BySport = []sportTotals{}

		yearSet := map[int]bool{}
		dayMap := map[string]*dayDTO{}
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
			// 逐日聚合覆盖**全部年份**（热力图「全部」模式要多年并排）
			if d, ok := dayMap[date]; ok {
				d.DistanceM += row.DistanceM
				d.DurationSecs += row.DurationSecs
				d.Count++
				if row.Sport == "run" {
					d.RunMeters += row.DistanceM
				} else if row.Sport == "ride" {
					d.RideMeters += row.DistanceM
				}
			} else {
				nd := &dayDTO{
					Date: date, DistanceM: row.DistanceM,
					DurationSecs: row.DurationSecs, Count: 1,
				}
				if row.Sport == "run" {
					nd.RunMeters = row.DistanceM
				} else if row.Sport == "ride" {
					nd.RideMeters = row.DistanceM
				}
				dayMap[date] = nd
				dayOrder = append(dayOrder, date)
			}
			if y == year {
				add(&out.Totals.Year)
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
	LabelKey  string  `json:"labelKey,omitempty"`
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

// LabelKey 给前端做本地化（半马/全马这两个词不是语言中立的）。
var pbTargets = []pbTarget{
	{Label: "1K", DistanceM: 1000},
	{Label: "5K", DistanceM: 5000},
	{Label: "10K", DistanceM: 10000},
	{Label: "半马", LabelKey: "race.half", DistanceM: 21097.5},
	{Label: "全马", LabelKey: "race.marathon", DistanceM: 42195},
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

		best, err := computePersonalBests(db, uint(athleteID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		out := make([]pbResult, 0, len(best))
		for _, b := range best {
			if b.Seconds > 0 {
				out = append(out, b)
			}
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// computePersonalBests 扫描全部跑步活动的速度流，返回各目标距离的最快分段。
// 抽出来供 /personal-best 与 /ai-prompt 共用（避免两处逻辑漂移）。
func computePersonalBests(db *gorm.DB, athleteID uint) ([]pbResult, error) {
	var acts []store.ActivityModel
	// 只算实际活动：计划课若带流（导入路径可以造出来）不该进个人最佳。
	if err := db.Where("athlete_id = ? AND sport = ? AND has_stream = ? AND is_planned = ?",
		athleteID, string(core.SportRun), true, false).
		Order("start_time ASC").Find(&acts).Error; err != nil {
		return nil, err
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
			// 优先用累计距离流（设备权威值）；缺失或不够长时退回速度积分。
			secs, ok := core.FastestOverDistance(stream.DistanceM, stream.RecIntervalSecs, t.DistanceM)
			if !ok {
				secs, ok = core.FastestForDistance(stream.SpeedKph, stream.RecIntervalSecs, t.DistanceM)
			}
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

	return best, nil
}
