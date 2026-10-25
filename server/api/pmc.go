package api

import (
	"net/http"
	"time"

	"github.com/openrunner/core"
	"github.com/openrunner/server/store"
	"gorm.io/gorm"
)

// handleAthletePMC 计算并返回 PMC 序列（票 10）。
// metric=trimp|rtss（默认 trimp）；区间与 GC 口径一致：
// from = min(最早活动, 最早赛季起点) − 1 天；to = max(最晚活动+365, 今天+365)。
func handleAthletePMC(db *gorm.DB) http.HandlerFunc {
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

		metric := r.URL.Query().Get("metric")
		if metric != "rtss" {
			metric = "trimp"
		}

		// 日负荷聚合：JOIN metrics，按活动本地日期逐日累加（实测/计划分流）
		rows, err := loadDailyLoads(db, uint(athleteID), metric)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "聚合失败")
			return
		}

		seasons, err := store.ListSeasons(db, uint(athleteID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}

		today := time.Now().Local().Format("2006-01-02")
		in := buildPMCInput(today, rows, seasons)
		series := core.ComputePMC(in)
		writeJSON(w, http.StatusOK, series)
	}
}

type dailyLoadRow struct {
	StartTime int64
	IsPlanned bool
	Value     float64
}

func loadDailyLoads(db *gorm.DB, athleteID uint, metric string) ([]dailyLoadRow, error) {
	var rows []dailyLoadRow
	err := db.Table("activities a").
		Select("a.start_time AS start_time, a.is_planned AS is_planned, m."+metric+" AS value").
		Joins("JOIN activity_metrics m ON m.activity_id = a.id").
		Where("a.athlete_id = ?", athleteID).
		Scan(&rows).Error
	return rows, err
}

func buildPMCInput(today string, rows []dailyLoadRow, seasons []store.SeasonModel) core.PMCInput {
	actual := map[string]float64{}
	planned := map[string]float64{}
	from, to := "", ""

	consider := func(d string) {
		if from == "" || d < from {
			from = d
		}
		if to == "" || d > to {
			to = d
		}
	}

	for _, row := range rows {
		d := timeToDateString(row.StartTime)
		if row.IsPlanned {
			planned[d] += row.Value
		} else {
			actual[d] += row.Value
		}
		consider(d)
	}

	seeds := make([]core.SeasonSeed, 0, len(seasons))
	for _, s := range seasons {
		if s.Seed > 0 {
			seeds = append(seeds, core.SeasonSeed{Date: s.StartDate, Value: s.Seed})
			consider(s.StartDate)
		}
	}
	consider(today)

	// GC 口径：to = max(最晚+365, 今天+365)；from 前移 1 天含首日活动
	in := core.PMCInput{
		Today:   today,
		LtsDays: 42,
		StsDays: 7,
		Actual:  actual,
		Planned: planned,
		Seeds:   seeds,
	}
	if from != "" {
		in.From = addDays(from, -1)
	}
	if to != "" {
		in.To = addDays(to, 365)
	}
	if tPlus := addDays(today, 365); in.To == "" || tPlus > in.To {
		in.To = tPlus
	}
	return in
}

func addDays(date string, n int) string {
	t, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		return date
	}
	return t.AddDate(0, 0, n).Format("2006-01-02")
}
