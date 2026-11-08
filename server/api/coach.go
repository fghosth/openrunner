package api

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/openrunner/core/coach"
	"gorm.io/gorm"

	"github.com/openrunner/server/store"
)

// coach week-check：把票 01 的规则引擎暴露为端点（coach-engine 票 02）。
//
// POST /api/athletes/{athleteId}/coach/week-check
//
//	{"week_role":"build","cautious":false,"from":"2026-11-09",
//	 "plan":[{"date":"2026-11-09","type":"long","minutes":90,"km":15,"quality_minutes":null}]}
//
// plan 缺省时取该周（默认下周一~周日）日历中的 isPlanned 跑步行；
// 历史固定取 planStart 前 8 周的实际跑步（timer 口径）。

var coachWeekRoles = map[string]bool{
	"base": true, "build": true, "peak": true,
	"deload": true, "taper": true, "race": true, "return": true,
}

func mondayOf(d time.Time) time.Time {
	return d.AddDate(0, 0, -int((d.Weekday()+6)%7))
}

func sessionMinutes(a store.ActivityModel) float64 {
	if a.TimerSecs > 0 {
		return float64(a.TimerSecs) / 60
	}
	return float64(a.DurationSecs) / 60
}

func handleCoachWeekCheck(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		var body struct {
			WeekRole string `json:"week_role"`
			Cautious bool   `json:"cautious"`
			From     string `json:"from"`
			Plan     []struct {
				Date    string   `json:"date"`
				Type    string   `json:"type"`
				Minutes float64  `json:"minutes"`
				KM      float64  `json:"km"`
				Quality *float64 `json:"quality_minutes"`
			} `json:"plan"`
		}
		if r.ContentLength != 0 && r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error())
				return
			}
		}
		role := body.WeekRole
		if role == "" {
			role = "build"
		}
		if !coachWeekRoles[role] {
			writeError(w, http.StatusBadRequest, "week_role 需为 base|build|peak|deload|taper|race|return")
			return
		}
		// 归一到本地午夜：手工创建的活动 start_time 是 00:00，
		// 若 planStart 带着「现在几点」会把周一早上的课切掉（实测踩过）。
		mon := mondayOf(time.Now()).AddDate(0, 0, 7) // 默认下周
		planStart := time.Date(mon.Year(), mon.Month(), mon.Day(), 0, 0, 0, 0, time.Local)
		if body.From != "" {
			d, err := time.ParseInLocation("2006-01-02", body.From, time.Local)
			if err != nil {
				writeError(w, http.StatusBadRequest, "from 需为 YYYY-MM-DD")
				return
			}
			planStart = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.Local)
		}

		acts, err := store.ListActivities(db, uint(athleteID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询活动失败")
			return
		}

		var plan []coach.Session
		if len(body.Plan) > 0 {
			for _, p := range body.Plan {
				if _, err := time.Parse("2006-01-02", p.Date); err != nil {
					writeError(w, http.StatusBadRequest, "plan[].date 需为 YYYY-MM-DD")
					return
				}
				s := coach.Session{Date: p.Date, Type: p.Type, Minutes: p.Minutes, KM: p.KM}
				if p.Quality != nil {
					q := *p.Quality
					s.QualityMinutes = &q
				}
				plan = append(plan, s)
			}
		} else {
			end := planStart.AddDate(0, 0, 7)
			for _, a := range acts {
				if !a.IsPlanned || a.Sport != "run" {
					continue
				}
				ts := time.Unix(a.StartTime, 0)
				if ts.Before(planStart) || !ts.Before(end) {
					continue
				}
				plan = append(plan, coach.Session{
					Date: ts.Format("2006-01-02"), Type: "easy",
					Minutes: sessionMinutes(a), KM: a.DistanceM / 1000,
				})
			}
		}
		if len(plan) == 0 {
			writeError(w, http.StatusBadRequest, "无计划：传 plan[]，或先把下周课写入日历")
			return
		}

		from := planStart.AddDate(0, 0, -56)
		var hist []coach.Session
		for _, a := range acts {
			if a.IsPlanned || a.Sport != "run" {
				continue
			}
			ts := time.Unix(a.StartTime, 0)
			if ts.Before(from) || !ts.Before(planStart) {
				continue
			}
			hist = append(hist, coach.Session{
				Date:    ts.Format("2006-01-02"),
				Minutes: sessionMinutes(a), KM: a.DistanceM / 1000,
			})
		}

		verdicts, err := coach.CheckWeek(coach.WeekCheckInput{
			WeekRole: role, Cautious: body.Cautious, History: hist, Plan: plan,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		var totalMin, totalKM, longMin float64
		var runs int
		for _, s := range plan {
			if s.Type == "rest" || s.Type == "cross" || s.Type == "strength" {
				continue
			}
			runs++
			totalMin += s.Minutes
			totalKM += s.KM
			if s.Minutes > longMin {
				longMin = s.Minutes
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"verdicts": verdicts,
			"week": map[string]any{
				"plan_start": planStart.Format("2006-01-02"),
				"week_role":  role,
				"runs":       runs, "total_min": totalMin,
				"total_km": totalKM, "long_min": longMin,
				"history_sessions": len(hist),
			},
		})
	}
}

// GET /api/athletes/{athleteId}/coach/paces —— 训练配速表（coach-engine 票 04）。
// 阈值来源优先级：pace_zones（当日生效）→ 最长比赛成绩反推 → 报错说明。
func handleCoachPaces(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		today := time.Now().Format("2006-01-02")

		var thrS float64
		var thrSource string
		if pz, err := store.PaceZoneForDate(db, uint(athleteID), today); err == nil && pz != nil {
			thrS = float64(pz.ThresholdPaceSecsPerKm)
			thrSource = "pace_zones"
		}
		var lthr float64
		if hz, err := store.HrZoneForDate(db, uint(athleteID), today); err == nil && hz != nil && hz.LtHr > 0 {
			lthr = float64(hz.LtHr)
		}

		results, _ := store.ListRaceResults(db, uint(athleteID))
		// VDOT 锚点取最长距离的比赛（最接近乳酸阈值的持续时长）；同距离取最快
		var bestKm, bestSecs float64
		for _, rr := range results {
			if rr.TimeSecs <= 0 || rr.DistanceM <= 0 {
				continue
			}
			if rr.DistanceM > bestKm || (rr.DistanceM == bestKm && float64(rr.TimeSecs) < bestSecs) {
				bestKm, bestSecs = rr.DistanceM/1000, float64(rr.TimeSecs)
			}
		}
		if thrS == 0 && bestKm > 0 {
			thrS = coach.ThresholdFromRace(bestKm, bestSecs)
			thrSource = "race"
		}
		if thrS == 0 {
			writeJSON(w, http.StatusOK, map[string]any{
				"error": "未配置阈值配速，且没有比赛成绩可反推——在设置页填阈值配速或录入比赛成绩",
			})
			return
		}

		resp := map[string]any{
			"threshold": map[string]any{
				"secsPerKm": thrS,
				"pace":      coach.FormatPace(thrS),
				"source":    thrSource,
			},
			"zones8020": coach.SpeedZoneTable(thrS, lthr),
		}
		if lthr > 0 {
			resp["lthr"] = lthr
		}
		if bestKm > 0 {
			vd, rows := coach.Daniels(bestKm, bestSecs)
			resp["vdot"] = map[string]any{
				"value": vd,
				"race":  map[string]any{"distanceM": bestKm * 1000, "timeSecs": bestSecs},
				"rows":  rows,
			}
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// GET /api/athletes/{athleteId}/coach/predict[?distance=10&time=48:48] —— 全马多模型预测
// （coach-engine 票 05）。缺省基准取最长比赛成绩；周跑量与训练配速从库聚合。
func handleCoachPredict(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		var baseKm, baseSecs float64
		baseSource := "query"
		if ds := r.URL.Query().Get("distance"); ds != "" {
			km, err1 := strconv.ParseFloat(ds, 64)
			ts := r.URL.Query().Get("time")
			secs, err2 := coach.ParseTime(ts)
			if err1 != nil || err2 != nil || km <= 0 {
				writeError(w, http.StatusBadRequest, "distance/time 参数非法（如 distance=10&time=48:48）")
				return
			}
			baseKm, baseSecs = km, secs
		} else {
			results, _ := store.ListRaceResults(db, uint(athleteID))
			for _, rr := range results {
				if rr.TimeSecs <= 0 || rr.DistanceM <= 0 {
					continue
				}
				if rr.DistanceM/1000 > baseKm || (rr.DistanceM/1000 == baseKm && float64(rr.TimeSecs) < baseSecs) {
					baseKm, baseSecs = rr.DistanceM/1000, float64(rr.TimeSecs)
				}
			}
			baseSource = "race"
			if baseKm == 0 {
				writeJSON(w, http.StatusOK, map[string]any{
					"error": "没有比赛成绩可作基准——传 distance+time 或在设置页录入成绩",
				})
				return
			}
		}

		// 周跑量与训练配速：近 9 周实际跑步（timer 口径）
		acts, err := store.ListActivities(db, uint(athleteID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询活动失败")
			return
		}
		cutoff := time.Now().AddDate(0, 0, -63)
		weeks := map[string]float64{}
		var totalDistM, totalSecs float64
		for _, a := range acts {
			if a.IsPlanned || a.Sport != "run" {
				continue
			}
			ts := time.Unix(a.StartTime, 0)
			if ts.Before(cutoff) {
				continue
			}
			mon := mondayOf(ts).Format("2006-01-02")
			weeks[mon] += a.DistanceM / 1000
			totalDistM += a.DistanceM
			totalSecs += sessionMinutes(a) * 60
		}
		var weeklyKm float64
		if len(weeks) > 0 {
			var sum float64
			for _, km := range weeks {
				sum += km
			}
			weeklyKm = sum / float64(len(weeks))
		}
		var trainingPace float64
		if totalDistM > 0 {
			trainingPace = totalSecs / (totalDistM / 1000)
		}

		// 第二场比赛（Vickers 模型 2）：与基准距离不同的最快成绩
		var r2Km, r2Secs float64
		if results, err := store.ListRaceResults(db, uint(athleteID)); err == nil {
			for _, rr := range results {
				km := rr.DistanceM / 1000
				if rr.TimeSecs <= 0 || km <= 0 || km == baseKm {
					continue
				}
				if r2Km == 0 || km > r2Km || (km == r2Km && float64(rr.TimeSecs) < r2Secs) {
					r2Km, r2Secs = km, float64(rr.TimeSecs)
				}
			}
		}

		pred := coach.PredictMarathon(coach.PredictInput{
			BaseKm: baseKm, BaseSecs: baseSecs,
			Race2Km: r2Km, Race2Secs: r2Secs,
			WeeklyKm: weeklyKm, TrainingPaceSecs: trainingPace,
			WeeksObserved: len(weeks),
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"base": map[string]any{
				"distanceM": baseKm * 1000, "timeSecs": baseSecs, "source": baseSource,
			},
			"weekly_km":      math.Round(weeklyKm*10) / 10,
			"weeks_observed": len(weeks),
			"training_pace":  trainingPace,
			"race2":          map[string]any{"distanceM": r2Km * 1000, "timeSecs": r2Secs},
			"prediction":     pred,
		})
	}
}
