package api

import (
	"encoding/json"
	"net/http"
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
