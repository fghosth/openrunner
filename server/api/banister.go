package api

import (
	"net/http"
	"time"

	"github.com/openrunner/core"
	"github.com/openrunner/server/store"
	"gorm.io/gorm"
)

// banisterRefDistance 是成绩归一化的参考距离（10 km）。
// 不同距离的成绩必须用 Riegel 公式换算到同一参考距离才能比较。
const banisterRefDistance = 10000.0

// handleBanister 用比赛成绩 + 日训练负荷拟合 Banister 表现模型，并给出未来预测。
//
// GET /api/athletes/{athleteId}/banister?metric=trimp|rtss
func handleBanister(db *gorm.DB) http.HandlerFunc {
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
		rows, err := loadDailyLoads(db, uint(athleteID), metric)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "聚合训练负荷失败")
			return
		}
		loads := map[string]float64{}
		for _, row := range rows {
			if row.IsPlanned {
				continue
			}
			loads[timeToDateString(row.StartTime)] += row.Value
		}

		results, err := store.ListRaceResults(db, uint(athleteID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询比赛成绩失败")
			return
		}
		if len(results) < 3 {
			writeJSON(w, http.StatusOK, map[string]any{
				"fitted": false,
				"reason": "需要至少 3 次比赛/计时成绩才能拟合（当前 " +
					itoa(int64(len(results))) + " 次）—— 去设置页录入，自己跑的 5K/10K 全力也算",
				"count":        len(results),
				"refDistanceM": banisterRefDistance,
			})
			return
		}

		races := make([]core.RacePoint, 0, len(results))
		for _, rr := range results {
			perf := core.RacePerformance(rr.DistanceM, float64(rr.TimeSecs), banisterRefDistance)
			if perf > 0 {
				races = append(races, core.RacePoint{Date: rr.Date, Performance: perf})
			}
		}
		model := core.FitBanister(loads, races)
		if model.RMSE < 0 {
			writeJSON(w, http.StatusOK, map[string]any{
				"fitted": false,
				"reason": "有效成绩不足（日期或数值异常），无法拟合",
				"count":  len(races),
			})
			return
		}

		// 历史点：实际 vs 模型预测
		type point struct {
			Date      string  `json:"date"`
			Actual    float64 `json:"actual"`    // 等价 10K 速度 m/s
			Predicted float64 `json:"predicted"` // 模型给出
			PaceSecs  float64 `json:"paceSecsPerKm"`
		}
		points := make([]point, 0, len(races))
		for _, rp := range races {
			points = append(points, point{
				Date:      rp.Date,
				Actual:    rp.Performance,
				Predicted: model.Predict(rp.Date),
				PaceSecs:  1000 / rp.Performance,
			})
		}

		// 未来 30 天预测（假设不训练，只看当前疲劳消退后的走势）
		type fc struct {
			Date      string  `json:"date"`
			Predicted float64 `json:"predicted"`
			PaceSecs  float64 `json:"paceSecsPerKm"`
		}
		forecast := make([]fc, 0, 30)
		today := time.Now().Local()
		for i := 1; i <= 30; i++ {
			d := today.AddDate(0, 0, i).Format("2006-01-02")
			p := model.Predict(d)
			if p <= 0 {
				continue
			}
			forecast = append(forecast, fc{Date: d, Predicted: p, PaceSecs: 1000 / p})
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"fitted":       true,
			"p0":           model.P0,
			"k1":           model.K1,
			"k2":           model.K2,
			"tau1":         model.Tau1,
			"tau2":         model.Tau2,
			"r2":           model.R2,
			"overfit":      model.Overfit,
			"rmse":         model.RMSE,
			"n":            model.N,
			"refDistanceM": banisterRefDistance,
			"points":       points,
			"forecast":     forecast,
		})
	}
}
