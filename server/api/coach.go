package api

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/openrunner/core"
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

// activityMinutes 活动的计时分钟数（timer 优先，与负荷指标口径一致）。
func activityMinutes(a store.ActivityModel) float64 {
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

		var plan []coach.TrainingUnit
		if len(body.Plan) > 0 {
			for _, p := range body.Plan {
				if _, err := time.Parse("2006-01-02", p.Date); err != nil {
					writeError(w, http.StatusBadRequest, "plan[].date 需为 YYYY-MM-DD")
					return
				}
				s := coach.TrainingUnit{Date: p.Date, Type: p.Type, Minutes: p.Minutes, KM: p.KM}
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
				plan = append(plan, coach.TrainingUnit{
					Date: ts.Format("2006-01-02"), Type: "easy",
					Minutes: activityMinutes(a), KM: a.DistanceM / 1000,
				})
			}
		}
		if len(plan) == 0 {
			writeError(w, http.StatusBadRequest, "无计划：传 plan[]，或先把下周课写入日历")
			return
		}

		from := planStart.AddDate(0, 0, -56)
		var hist []coach.TrainingUnit
		for _, a := range acts {
			if a.IsPlanned || a.Sport != "run" {
				continue
			}
			ts := time.Unix(a.StartTime, 0)
			if ts.Before(from) || !ts.Before(planStart) {
				continue
			}
			hist = append(hist, coach.TrainingUnit{
				Date:    ts.Format("2006-01-02"),
				Minutes: activityMinutes(a), KM: a.DistanceM / 1000,
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
// 阈值来源优先级见 thresholdForDate；取不到返回 {error} 说明，不猜。
func handleCoachPaces(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		today := time.Now().Format("2006-01-02")
		thrS, thrSource := thresholdForDate(db, uint(athleteID), today)
		if thrS == 0 {
			writeJSON(w, http.StatusOK, map[string]any{
				"error": "未配置阈值配速，且没有比赛成绩可反推——在设置页填阈值配速或录入比赛成绩",
			})
			return
		}
		var lthr float64
		if hz, err := store.HrZoneForDate(db, uint(athleteID), today); err == nil && hz != nil && hz.LtHr > 0 {
			lthr = float64(hz.LtHr)
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
		if bestKm, bestSecs, ok := longestRace(db, uint(athleteID)); ok {
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
// （coach-engine 票 05）。缺省基准取最长比赛成绩；query 可覆盖基准。
// 周跑量/训练配速/第二场比赛的聚合口径见 predictInputsFromDB（与 ai-prompt 共用）。
func handleCoachPredict(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		in, ok := predictInputsFromDB(db, uint(athleteID))
		baseSource := "race"
		if ds := r.URL.Query().Get("distance"); ds != "" {
			km, err1 := strconv.ParseFloat(ds, 64)
			secs, err2 := coach.ParseTime(r.URL.Query().Get("time"))
			if err1 != nil || err2 != nil || km <= 0 {
				writeError(w, http.StatusBadRequest, "distance/time 参数非法（如 distance=10&time=48:48）")
				return
			}
			in.BaseKm, in.BaseSecs = km, secs
			baseSource = "query"
			ok = true
		}
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{
				"error": "没有比赛成绩可作基准——传 distance+time 或在设置页录入成绩",
			})
			return
		}
		pred := coach.PredictMarathon(in)
		writeJSON(w, http.StatusOK, map[string]any{
			"base": map[string]any{
				"distanceM": in.BaseKm * 1000, "timeSecs": in.BaseSecs, "source": baseSource,
			},
			"weekly_km":      math.Round(in.WeeklyKm*10) / 10,
			"weeks_observed": in.WeeksObserved,
			"training_pace":  in.TrainingPaceSecs,
			"race2":          map[string]any{"distanceM": in.Race2Km * 1000, "timeSecs": in.Race2Secs},
			"prediction":     pred,
		})
	}
}

// GET /api/athletes/{athleteId}/activities/{activityId}/coach/analysis —— 单课分析（票 06）。
// 三指标（Pa:Hr 解耦 / EF / 心率漂移）；不满足适用条件时 applicable=false + 原因，不 500。
func handleCoachAnalysis(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err1 := pathValueID(r, "athleteId")
		activityID, err2 := pathValueID(r, "activityId")
		if err1 != nil || err2 != nil {
			writeError(w, http.StatusBadRequest, "非法 id")
			return
		}
		m, err := store.GetActivity(db, uint(athleteID), uint(activityID))
		if err != nil || m == nil {
			writeError(w, http.StatusNotFound, "活动不存在")
			return
		}
		sm, err := store.GetStream(db, uint(activityID))
		if err != nil || sm == nil {
			writeJSON(w, http.StatusOK, coach.ActivityAnalysis{
				Reason: "该活动没有原始流数据（导入时未含 stream 或已清理）",
			})
			return
		}
		stream, err := core.UnmarshalStream(sm.Data)
		if err != nil {
			writeJSON(w, http.StatusOK, coach.ActivityAnalysis{
				Reason: "流数据解析失败",
			})
			return
		}
		dur := float64(m.TimerSecs)
		if dur <= 0 {
			dur = float64(m.DurationSecs)
		}
		writeJSON(w, http.StatusOK, coach.AnalyzeActivity(dur, stream.RecIntervalSecs, stream.HeartRate, stream.SpeedKph))
	}
}

// —— 票 07：以下 helper 供端点与 ai-prompt 共用（一处口径，两处消费）——

// thresholdForDate 阈值配速来源优先级：pace_zones（当日生效）→ 最长比赛反推。
// 取不到返回 (0, "")，调用方负责给原因，不猜。
func thresholdForDate(db *gorm.DB, athleteID uint, today string) (float64, string) {
	if pz, err := store.PaceZoneForDate(db, athleteID, today); err == nil && pz != nil {
		return float64(pz.ThresholdPaceSecsPerKm), "pace_zones"
	}
	if km, secs, ok := longestRace(db, athleteID); ok {
		return coach.ThresholdFromRace(km, secs), "race"
	}
	return 0, ""
}

// longestRace 最长距离的比赛（同距离取最快）。
func longestRace(db *gorm.DB, athleteID uint) (km, secs float64, ok bool) {
	results, _ := store.ListRaceResults(db, athleteID)
	for _, rr := range results {
		if rr.TimeSecs <= 0 || rr.DistanceM <= 0 {
			continue
		}
		if rr.DistanceM/1000 > km || (rr.DistanceM/1000 == km && float64(rr.TimeSecs) < secs) {
			km, secs = rr.DistanceM/1000, float64(rr.TimeSecs)
		}
	}
	return km, secs, km > 0
}

// predictInputsFromDB 从库聚合预测输入：基准=最长比赛、周跑量/训练配速=近 9 周
// 实际跑步（timer 口径、ISO 周分桶）、第二场=与基准距离不同的最快成绩。
func predictInputsFromDB(db *gorm.DB, athleteID uint) (coach.PredictInput, bool) {
	var in coach.PredictInput
	in.BaseKm, in.BaseSecs, _ = longestRace(db, athleteID)
	// 周跑量/训练配速即使没有基准也算（query 覆盖基准的路径要用）
	acts, err := store.ListActivities(db, athleteID)
	if err != nil {
		return in, in.BaseKm > 0
	}
	// 只统计已完成的周：Vickers/Tanda 的定义是第 -9~-1 周，
	// 含当前不完整周会在周初系统性低估周跑量（review 抓出）
	mn := mondayOf(time.Now())
	thisMonday := time.Date(mn.Year(), mn.Month(), mn.Day(), 0, 0, 0, 0, time.Local)
	cutoff := thisMonday.AddDate(0, 0, -63)
	weeks := map[string]float64{}
	var totalDistM, totalSecs float64
	for _, a := range acts {
		if a.IsPlanned || a.Sport != "run" {
			continue
		}
		ts := time.Unix(a.StartTime, 0)
		if ts.Before(cutoff) || !ts.Before(thisMonday) {
			continue
		}
		weeks[mondayOf(ts).Format("2006-01-02")] += a.DistanceM / 1000
		totalDistM += a.DistanceM
		totalSecs += activityMinutes(a) * 60
	}
	for _, km := range weeks {
		in.WeeklyKm += km
	}
	if len(weeks) > 0 {
		in.WeeklyKm /= float64(len(weeks))
		in.WeeksObserved = len(weeks)
	}
	if totalDistM > 0 {
		in.TrainingPaceSecs = totalSecs / (totalDistM / 1000)
	}
	results, _ := store.ListRaceResults(db, athleteID)
	for _, rr := range results {
		km := rr.DistanceM / 1000
		if rr.TimeSecs <= 0 || km <= 0 || km == in.BaseKm {
			continue
		}
		if km > in.Race2Km || (km == in.Race2Km && float64(rr.TimeSecs) < in.Race2Secs) {
			in.Race2Km, in.Race2Secs = km, float64(rr.TimeSecs)
		}
	}
	return in, in.BaseKm > 0
}

// weekCheckFromCalendar 用日历里 planStart 那一周的计划课跑规则引擎（提示词用）。
// 无计划课返回 nil。
func weekCheckFromCalendar(db *gorm.DB, athleteID uint, planStart time.Time) []coach.Verdict {
	// 归一到本地午夜（Round 36 同款坑：带时刻的周一会切掉当天更早的课）
	planStart = time.Date(planStart.Year(), planStart.Month(), planStart.Day(), 0, 0, 0, 0, time.Local)
	acts, err := store.ListActivities(db, athleteID)
	if err != nil {
		return nil
	}
	end := planStart.AddDate(0, 0, 7)
	var plan, hist []coach.TrainingUnit
	for _, a := range acts {
		ts := time.Unix(a.StartTime, 0)
		if a.IsPlanned && a.Sport == "run" && !ts.Before(planStart) && ts.Before(end) {
			plan = append(plan, coach.TrainingUnit{
				Date: ts.Format("2006-01-02"), Type: "easy",
				Minutes: activityMinutes(a), KM: a.DistanceM / 1000,
			})
		}
		if !a.IsPlanned && a.Sport == "run" && !ts.Before(planStart.AddDate(0, 0, -56)) && ts.Before(planStart) {
			hist = append(hist, coach.TrainingUnit{
				Date:    ts.Format("2006-01-02"),
				Minutes: activityMinutes(a), KM: a.DistanceM / 1000,
			})
		}
	}
	if len(plan) == 0 {
		return nil
	}
	verdicts, err := coach.CheckWeek(coach.WeekCheckInput{
		WeekRole: "build", History: hist, Plan: plan,
	})
	if err != nil {
		return nil
	}
	return verdicts
}
