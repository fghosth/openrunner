package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/openrunner/core"
	"github.com/openrunner/server/store"
	"gorm.io/gorm"
)

// metricsDTO 活动度量（票 05 心率链 + 票 06 配速链）。
type metricsDTO struct {
	AvgHr       float64   `json:"avgHr"`
	Trimp       float64   `json:"trimp"`
	Trimp100    float64   `json:"trimp100"`
	TrimpZonal  float64   `json:"trimpZonal"`
	HrTiZ       []float64 `json:"hrTiZ"`
	Rtss        float64   `json:"rtss"`
	Np          float64   `json:"np"`
	PowerLoad   float64   `json:"powerLoad"`
	PowerTiZ    []float64 `json:"powerTiZ"`
	PaceTiZ     []float64 `json:"paceTiZ"`
	IsEstimated bool      `json:"isEstimated"`
}

// computeMetrics 计算并落库活动度量。
// 心率链（票 05）与配速链（票 06）各自独立可选：对应区间未配置或数据缺失时跳过该链。
// stream == nil（summary-only，票 07）走估算路径：avgHR/平均速度代入同一公式，IsEstimated=true；
// 两条链都无产出时不写 metrics（detail 显示 null，UI 引导配置）。
func computeMetrics(db *gorm.DB, athlete *store.AthleteModel, activity *store.ActivityModel, stream *core.Stream) error {
	date := timeToDateString(activity.StartTime)
	m := store.ActivityMetricsModel{ActivityID: activity.ID, IsEstimated: stream == nil}
	produced := false

	// ---- 心率链 ----
	if zone, err := store.HrZoneForDate(db, athlete.ID, date); err == nil {
		avgHr := 0.0
		if stream != nil {
			avgHr = stream.AvgHeartRate()
		} else {
			avgHr = activity.AvgHr // 估算源：手工均值
		}
		if avgHr > 0 {
			ksex := 1.92
			if athlete.Sex == "F" {
				ksex = 1.67
			}
			durationSecs := effectiveDurationSecs(activity)
			m.AvgHr = avgHr
			m.Trimp = core.TrimpPoints(durationSecs, avgHr, float64(zone.MaxHr), float64(zone.RestHr), ksex)
			m.Trimp100 = core.Trimp100(m.Trimp, float64(zone.MaxHr), float64(zone.RestHr), float64(zone.LtHr), ksex)
			if stream != nil {
				m.TrimpZonal = core.TrimpZonal(stream.HeartRate, float64(zone.MaxHr), stream.RecIntervalSecs)
				tiz := core.HrTimeInZone(stream.HeartRate, float64(zone.MaxHr), stream.RecIntervalSecs)
				if b, err := json.Marshal(tiz); err == nil {
					m.HrTiZJSON = string(b)
				}
			}
			produced = true
		}
	} else if !errors.Is(err, store.ErrHrZoneNotFound) {
		return err
	}
	// ---- 功率链（票 09）：有功率流 + power 域 CP/W' 配置 ----
	// NP=功率 4 次方均值；PowerLoad=类 BikeScore（100·hours·IF²，IF=NP/CP）；
	// PowerTiZ 沿用 %CV 6 区界（比值运算，速度/功率通用）。
	hasPower := false
	if stream != nil { // 估算路径（stream==nil）无功率数据
		for _, p := range stream.Power {
			if p > 0 {
				hasPower = true
				break
			}
		}
	}
	if hasPower {
		// NP（标准化功率）只依赖功率流本身 —— 4 次方均值，与 CP 无关。
		// 之前把它和 CP 绑在一起，导致「有功率数据但没配 power 域 CP」时 NP 也空
		// （实测 206 条活动有功率流，NP 却全是 0）。
		// NP 用 Coggan 口径（30 秒滑动平均 + 4 次方均值），不是裸 4 次方均值：
		// 缺那道平均时单个功率伪影能把 NP 抬高 49%（实测活动 281），
		// 而下面 PowerLoad 用 IF²，误差会被平方放大。见 core.NormalizedPower 注释。
		np := core.NormalizedPower(stream.Power, stream.RecIntervalSecs)
		if np > 0 {
			m.Np = np
			produced = true
		}
		// PowerLoad / PowerTiZ 才需要 CP（强度因子 = NP/CP，区间按 %CP 分界）
		if pz, err := store.CsZoneForDate(db, athlete.ID, "power", date); err == nil {
			cp := pz.Cs
			if np > 0 && cp > 0 {
				durationSecs := effectiveDurationSecs(activity)
				m.PowerLoad = core.Rtss(durationSecs, np, cp)
				// 功率 TiZ 用 Coggan 七区（%CP：55/75/90/105/120/150），
				// 不是跑步配速的 %CV 界——那套会把耐力骑整段压进 W1。
				ptiz := core.PowerTimeInZone(stream.Power, cp, stream.RecIntervalSecs)
				if b, err := json.Marshal(ptiz); err == nil {
					m.PowerTiZJSON = string(b)
				}
			}
		} else if !errors.Is(err, store.ErrCsZoneNotFound) {
			return err
		}
	}

	// ---- 配速链（仅跑步；骑行用功率链，其他运动无配速强度语义）----
	// 口径说明（继承 GC）：TRIMP 用 Timer×avgHR，Zonal/TiZ 逐样本——流中停表段若仍有心率会被计入
	// TiZ，与 GC 同源；rTSS 无 NGP 坡度修正（简化，山地跑偏差记 DEVLOG，PMC 票前补口径）。
	if activity.Sport == string(core.SportRun) {
		if pz, err := store.PaceZoneForDate(db, athlete.ID, date); err == nil {
			cv := 3600.0 / float64(pz.ThresholdPaceSecsPerKm) // 秒/km → km/h
			np := 0.0
			if stream != nil {
				// 与 NP 同口径（30 秒滑动平均 + 4 次方均值）：GPS 速度尖峰和功率伪影
				// 是同一类垃圾，裸 4 次方均值实测能把 rTSS 抬 43.6%（活动 281）。
				np = core.NormalizedSpeed(stream.SpeedKph, stream.RecIntervalSecs)
			} else if activity.DurationSecs > 0 && activity.DistanceM > 0 {
				// 估算：平均速度（elapsed 基准——summary-only 无 Timer 概念，见 DEVLOG Round 5）
				np = activity.DistanceM / 1000 / (float64(activity.DurationSecs) / 3600)
			}
			if np > 0 {
				m.Rtss = core.Rtss(effectiveDurationSecs(activity), np, cv)
				if stream != nil {
					ptiz := core.PaceTimeInZone(stream.SpeedKph, cv, stream.RecIntervalSecs)
					if b, err := json.Marshal(ptiz); err == nil {
						m.PaceTiZJSON = string(b)
					}
				}
				produced = true
			}
		} else if !errors.Is(err, store.ErrPaceZoneNotFound) {
			return err
		}
	}

	if !produced {
		return nil
	}
	return upsertActivityMetrics(db, &m)
}

func upsertActivityMetrics(db *gorm.DB, m *store.ActivityMetricsModel) error {
	return db.Save(m).Error
}

// getMetricsDTO 读取活动 metrics（无则 nil）。
func getMetricsDTO(db *gorm.DB, activityID uint) (*metricsDTO, error) {
	var m store.ActivityMetricsModel
	if err := db.Where("activity_id = ?", activityID).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var tiz []float64
	_ = json.Unmarshal([]byte(m.HrTiZJSON), &tiz)
	if tiz == nil {
		tiz = make([]float64, 5)
	}
	var ptiz []float64
	_ = json.Unmarshal([]byte(m.PaceTiZJSON), &ptiz)
	if ptiz == nil {
		ptiz = make([]float64, 6)
	}
	var powtiz []float64
	_ = json.Unmarshal([]byte(m.PowerTiZJSON), &powtiz)
	if len(powtiz) < 7 { // 旧行是 6 桶（跑步区界），补齐到 Coggan 七区
		powtiz = make([]float64, 7)
	}
	return &metricsDTO{
		AvgHr: m.AvgHr, Trimp: m.Trimp, Trimp100: m.Trimp100,
		TrimpZonal: m.TrimpZonal, HrTiZ: tiz,
		Rtss: m.Rtss, Np: m.Np, PowerLoad: m.PowerLoad, PowerTiZ: powtiz,
		PaceTiZ: ptiz, IsEstimated: m.IsEstimated,
	}, nil
}

// recomputeMetrics 对单个活动重算全部度量（有 Stream 走实测，无 Stream 走估算）。
func recomputeMetrics(db *gorm.DB, athlete *store.AthleteModel, activity *store.ActivityModel) error {
	sm, err := store.GetStream(db, activity.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return computeMetrics(db, athlete, activity, nil) // summary-only 估算
	}
	if err != nil {
		return err
	}
	stream, err := core.UnmarshalStream(sm.Data)
	if err != nil {
		return err
	}
	return computeMetrics(db, athlete, activity, &stream)
}

// effectiveDurationSecs 负荷指标的时长口径：优先 TimerSecs（不含停表），回退 elapsed。
// 心率链与配速链共用（Round 5：消除两链重复回退块）。
func effectiveDurationSecs(m *store.ActivityModel) float64 {
	if m.TimerSecs > 0 {
		return float64(m.TimerSecs)
	}
	return float64(m.DurationSecs)
}

// handleRecomputeAll 批量重算档案全部活动的指标（票 15 补充）。
// 场景：用户配好心率区间/阈值配速/CS 后，历史活动需要按新参数重算——
// 否则趋势页（PMC）永远没有日负荷数据。
func handleRecomputeAll(db *gorm.DB) http.HandlerFunc {
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
		acts, err := store.ListActivities(db, uint(athleteID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询活动失败")
			return
		}

		computed, skipped, failed := 0, 0, 0
		for i := range acts {
			a := &acts[i]
			before, _ := getMetricsDTO(db, a.ID)
			if err := recomputeMetrics(db, athlete, a); err != nil {
				failed++
				continue
			}
			after, _ := getMetricsDTO(db, a.ID)
			if after != nil && (before == nil || after.Trimp != before.Trimp || after.Rtss != before.Rtss ||
				after.PowerLoad != before.PowerLoad) {
				computed++
			} else if after == nil {
				skipped++ // 无区间配置或无数据 → 无指标
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"total": len(acts), "computed": computed, "skipped": skipped, "failed": failed,
		})
	}
}
