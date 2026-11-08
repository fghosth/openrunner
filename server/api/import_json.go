package api

// 导入设置页导出的 JSON 归档（debt 用户反馈：导出有、导入无，无法迁移/恢复）。
//
// 语义是「合并」而非「覆盖重置」：
//   - 活动按 (start_time, sport, duration, distance) 去重，重复跳过；
//   - 区间/赛季/比赛成绩按全字段等值去重；
//   - 档案设置（性别/目标/简介/头像）应用到目标档案，名字不动（目标档案由用户选定）；
//   - 指标不搬运——导入后按当前算法与区间重算（指标是派生数据，口径以本机为准）。
//
// 兼容 schema v1（缺 planType/raceResults/goals 字段，按零值处理）。

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/openrunner/core"
	"github.com/openrunner/server/store"
	"gorm.io/gorm"
)

// importResult 导入计数（UI 反馈用）。
type importResult struct {
	ActivitiesImported int  `json:"activitiesImported"`
	ActivitiesSkipped  int  `json:"activitiesSkipped"`
	ZonesImported      int  `json:"zonesImported"`
	ZonesSkipped       int  `json:"zonesSkipped"`
	SeasonsImported    int  `json:"seasonsImported"`
	RacesImported      int  `json:"racesImported"`
	ProfileUpdated     bool `json:"profileUpdated"`
}

func handleImportJSON(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		athlete, err := store.GetAthlete(db, uint(athleteID))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusNotFound, "Athlete 不存在")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 128<<20) // 归档含 base64 流，上限放宽到 128MB
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "读取请求体失败（文件过大？）")
			return
		}
		var doc struct {
			SchemaVersion int                     `json:"schemaVersion"`
			Athlete       exportAthlete           `json:"athlete"`
			HrZones       []store.HrZoneModel     `json:"hrZones"`
			PaceZones     []store.PaceZoneModel   `json:"paceZones"`
			CsZones       []store.CsZoneModel     `json:"csZones"`
			Seasons       []store.SeasonModel     `json:"seasons"`
			RaceResults   []store.RaceResultModel `json:"raceResults"`
			Activities    []exportActivity        `json:"activities"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			writeError(w, http.StatusBadRequest, "不是合法的导出 JSON: "+err.Error())
			return
		}
		if doc.SchemaVersion < 1 || doc.SchemaVersion > exportSchemaVersion {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("schemaVersion %d 不支持（本机构支持 1~%d）", doc.SchemaVersion, exportSchemaVersion))
			return
		}

		var res importResult

		// 1) 档案设置（名字不动）
		athlete.Sex = normSex(doc.Athlete.Sex)
		athlete.AvatarURL = doc.Athlete.AvatarURL
		athlete.Bio = doc.Athlete.Bio
		athlete.YearlyGoalKm = doc.Athlete.YearlyGoalKm
		athlete.MonthlyGoalKm = doc.Athlete.MonthlyGoalKm
		athlete.WeeklyGoalKm = doc.Athlete.WeeklyGoalKm
		if err := db.Model(athlete).Updates(map[string]any{
			"sex": athlete.Sex, "avatar_url": athlete.AvatarURL, "bio": athlete.Bio,
			"yearly_goal_km": athlete.YearlyGoalKm, "monthly_goal_km": athlete.MonthlyGoalKm,
			"weekly_goal_km": athlete.WeeklyGoalKm,
		}).Error; err != nil {
			writeError(w, http.StatusInternalServerError, "档案设置更新失败")
			return
		}
		res.ProfileUpdated = true

		// 2) 区间/赛季/比赛成绩：全字段等值去重
		hrExist := hrZoneKeySet(db, uint(athleteID))
		for _, z := range doc.HrZones {
			key := fmt.Sprintf("%s|%s|%d|%d|%d", z.ValidFrom, z.ValidTo, z.MaxHr, z.RestHr, z.LtHr)
			if hrExist[key] {
				res.ZonesSkipped++
				continue
			}
			nz := store.HrZoneModel{AthleteID: uint(athleteID), ValidFrom: z.ValidFrom, ValidTo: z.ValidTo, MaxHr: z.MaxHr, RestHr: z.RestHr, LtHr: z.LtHr}
			if err := db.Create(&nz).Error; err != nil {
				writeError(w, http.StatusInternalServerError, "心率区间导入失败")
				return
			}
			hrExist[key] = true
			res.ZonesImported++
		}
		paceExist := paceZoneKeySet(db, uint(athleteID))
		for _, z := range doc.PaceZones {
			key := fmt.Sprintf("%s|%s|%d", z.ValidFrom, z.ValidTo, z.ThresholdPaceSecsPerKm)
			if paceExist[key] {
				res.ZonesSkipped++
				continue
			}
			nz := store.PaceZoneModel{AthleteID: uint(athleteID), ValidFrom: z.ValidFrom, ValidTo: z.ValidTo, ThresholdPaceSecsPerKm: z.ThresholdPaceSecsPerKm}
			if err := db.Create(&nz).Error; err != nil {
				writeError(w, http.StatusInternalServerError, "阈值配速导入失败")
				return
			}
			paceExist[key] = true
			res.ZonesImported++
		}
		csExist := csZoneKeySet(db, uint(athleteID))
		for _, z := range doc.CsZones {
			key := fmt.Sprintf("%s|%s|%s|%.4f|%.2f|%s", z.Domain, z.ValidFrom, z.ValidTo, z.Cs, z.Dprime, z.Source)
			if csExist[key] {
				res.ZonesSkipped++
				continue
			}
			nz := store.CsZoneModel{AthleteID: uint(athleteID), Domain: z.Domain, ValidFrom: z.ValidFrom, ValidTo: z.ValidTo, Cs: z.Cs, Dprime: z.Dprime, Source: z.Source}
			if err := db.Create(&nz).Error; err != nil {
				writeError(w, http.StatusInternalServerError, "临界域导入失败")
				return
			}
			csExist[key] = true
			res.ZonesImported++
		}
		seaExist := seasonKeySet(db, uint(athleteID))
		for _, sc := range doc.Seasons {
			key := fmt.Sprintf("%s|%s|%s|%.2f", sc.Name, sc.StartDate, sc.EndDate, sc.Seed)
			if seaExist[key] {
				continue
			}
			ns := store.SeasonModel{AthleteID: uint(athleteID), Name: sc.Name, StartDate: sc.StartDate, EndDate: sc.EndDate, Seed: sc.Seed}
			if err := db.Create(&ns).Error; err != nil {
				writeError(w, http.StatusInternalServerError, "赛季导入失败")
				return
			}
			seaExist[key] = true
			res.SeasonsImported++
		}
		raceExist := raceKeySet(db, uint(athleteID))
		for _, rr := range doc.RaceResults {
			key := fmt.Sprintf("%s|%.1f|%d|%s", rr.Date, rr.DistanceM, rr.TimeSecs, rr.Name)
			if raceExist[key] {
				continue
			}
			nr := store.RaceResultModel{AthleteID: uint(athleteID), Date: rr.Date, DistanceM: rr.DistanceM, TimeSecs: rr.TimeSecs, Name: rr.Name, Note: rr.Note}
			if err := db.Create(&nr).Error; err != nil {
				writeError(w, http.StatusInternalServerError, "比赛成绩导入失败")
				return
			}
			raceExist[key] = true
			res.RacesImported++
		}

		// 3) 活动：(start_time, sport, duration, distance) 去重；流解码入库；指标重算
		actExist := activityKeySet(db, uint(athleteID))
		for _, ea := range doc.Activities {
			key := fmt.Sprintf("%d|%s|%d|%.1f", ea.StartTime, ea.Sport, ea.DurationSecs, ea.DistanceM)
			if actExist[key] {
				res.ActivitiesSkipped++
				continue
			}
			act := core.Activity{
				Sport:         core.Sport(ea.Sport),
				StartTime:     time.Unix(ea.StartTime, 0),
				Duration:      time.Duration(ea.DurationSecs) * time.Second,
				TimerSecs:     ea.TimerSecs,
				Distance:      ea.DistanceM,
				ElevationGain: ea.ElevationGainM,
				IsPlanned:     ea.IsPlanned,
				AvgHr:         ea.AvgHr,
			}
			if err := act.Validate(); err != nil {
				res.ActivitiesSkipped++ // 脏记录跳过（如 sport 不在收录范围）
				continue
			}
			var stream core.Stream
			if ea.Stream != "" {
				blob, derr := base64.StdEncoding.DecodeString(ea.Stream)
				if derr != nil {
					res.ActivitiesSkipped++
					continue
				}
				st, serr := core.UnmarshalStream(blob)
				if serr != nil {
					res.ActivitiesSkipped++
					continue
				}
				stream = st
			}
			id, cerr := store.CreateActivityWithStream(db, uint(athleteID), act, stream, ea.Source, "")
			if cerr != nil {
				writeError(w, http.StatusInternalServerError, "活动导入失败")
				return
			}
			if ea.IsPlanned && ea.PlanType != "" {
				_ = store.SetPlanType(db, uint(athleteID), id, ea.PlanType)
			}
			if m, gerr := store.GetActivity(db, uint(athleteID), id); gerr == nil {
				_ = recomputeMetrics(db, athlete, m) // 指标按本机当前算法/区间重算
			}
			actExist[key] = true
			res.ActivitiesImported++
		}

		writeJSON(w, http.StatusOK, res)
	}
}

func normSex(s string) string {
	if s == "F" {
		return "F"
	}
	return "M"
}

func hrZoneKeySet(db *gorm.DB, athleteID uint) map[string]bool {
	out := map[string]bool{}
	if zs, err := store.ListHrZones(db, athleteID); err == nil {
		for _, z := range zs {
			out[fmt.Sprintf("%s|%s|%d|%d|%d", z.ValidFrom, z.ValidTo, z.MaxHr, z.RestHr, z.LtHr)] = true
		}
	}
	return out
}

func paceZoneKeySet(db *gorm.DB, athleteID uint) map[string]bool {
	out := map[string]bool{}
	if zs, err := store.ListPaceZones(db, athleteID); err == nil {
		for _, z := range zs {
			out[fmt.Sprintf("%s|%s|%d", z.ValidFrom, z.ValidTo, z.ThresholdPaceSecsPerKm)] = true
		}
	}
	return out
}

func csZoneKeySet(db *gorm.DB, athleteID uint) map[string]bool {
	out := map[string]bool{}
	if zs, err := store.ListCsZones(db, athleteID); err == nil {
		for _, z := range zs {
			out[fmt.Sprintf("%s|%s|%s|%.4f|%.2f|%s", z.Domain, z.ValidFrom, z.ValidTo, z.Cs, z.Dprime, z.Source)] = true
		}
	}
	return out
}

func seasonKeySet(db *gorm.DB, athleteID uint) map[string]bool {
	out := map[string]bool{}
	if zs, err := store.ListSeasons(db, athleteID); err == nil {
		for _, z := range zs {
			out[fmt.Sprintf("%s|%s|%s|%.2f", z.Name, z.StartDate, z.EndDate, z.Seed)] = true
		}
	}
	return out
}

func raceKeySet(db *gorm.DB, athleteID uint) map[string]bool {
	out := map[string]bool{}
	if rs, err := store.ListRaceResults(db, athleteID); err == nil {
		for _, r := range rs {
			out[fmt.Sprintf("%s|%.1f|%d|%s", r.Date, r.DistanceM, r.TimeSecs, r.Name)] = true
		}
	}
	return out
}

func activityKeySet(db *gorm.DB, athleteID uint) map[string]bool {
	out := map[string]bool{}
	if as, err := store.ListActivities(db, athleteID); err == nil {
		for _, a := range as {
			out[fmt.Sprintf("%d|%s|%d|%.1f", a.StartTime, a.Sport, a.DurationSecs, a.DistanceM)] = true
		}
	}
	return out
}
