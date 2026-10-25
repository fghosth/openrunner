package api

import (
	"encoding/json"
	"errors"

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
	PaceTiZ     []float64 `json:"paceTiZ"`
	IsEstimated bool      `json:"isEstimated"`
}

// computeMetrics 计算并落库活动度量。
// 心率链（票 05）与配速链（票 06）各自独立可选：对应区间未配置或数据缺失时跳过该链，
// 两条链都无产出时不写 metrics（detail 显示 null，UI 引导配置）。
func computeMetrics(db *gorm.DB, athlete *store.AthleteModel, activity *store.ActivityModel, stream core.Stream) error {
	date := timeToDateString(activity.StartTime)
	m := store.ActivityMetricsModel{ActivityID: activity.ID}
	produced := false

	// ---- 心率链 ----
	if zone, err := store.HrZoneForDate(db, athlete.ID, date); err == nil {
		if avgHr := stream.AvgHeartRate(); avgHr > 0 {
			ksex := 1.92
			if athlete.Sex == "F" {
				ksex = 1.67
			}
			durationSecs := float64(activity.TimerSecs)
			if durationSecs <= 0 {
				durationSecs = float64(activity.DurationSecs) // 回退（手工活动可能无 Timer）
			}
			m.AvgHr = avgHr
			m.Trimp = core.TrimpPoints(durationSecs, avgHr, float64(zone.MaxHr), float64(zone.RestHr), ksex)
			m.Trimp100 = core.Trimp100(m.Trimp, float64(zone.MaxHr), float64(zone.RestHr), float64(zone.LtHr), ksex)
			m.TrimpZonal = core.TrimpZonal(stream.HeartRate, float64(zone.MaxHr), stream.RecIntervalSecs)
			tiz := core.HrTimeInZone(stream.HeartRate, float64(zone.MaxHr), stream.RecIntervalSecs)
			if b, err := json.Marshal(tiz); err == nil {
				m.HrTiZJSON = string(b)
			}
			produced = true
		}
	} else if !errors.Is(err, store.ErrHrZoneNotFound) {
		return err
	}
	// ---- 配速链（仅跑步；骑行用功率链，其他运动无配速强度语义）----
	// 口径说明（继承 GC）：TRIMP 用 Timer×avgHR，Zonal/TiZ 逐样本——流中停表段若仍有心率会被计入
	// TiZ，与 GC 同源；rTSS 无 NGP 坡度修正（简化，山地跑偏差记 DEVLOG，PMC 票前补口径）。
	if activity.Sport == string(core.SportRun) {
		if pz, err := store.PaceZoneForDate(db, athlete.ID, date); err == nil {
			cv := 3600.0 / float64(pz.ThresholdPaceSecsPerKm) // 秒/km → km/h
			np := core.NormalizedSpeedKph(stream.SpeedKph)
			if np > 0 {
				durationSecs := float64(activity.TimerSecs)
				if durationSecs <= 0 {
					durationSecs = float64(activity.DurationSecs) // 与心率链同回退
				}
				m.Rtss = core.Rtss(durationSecs, np, cv)
				ptiz := core.PaceTimeInZone(stream.SpeedKph, cv, stream.RecIntervalSecs)
				if b, err := json.Marshal(ptiz); err == nil {
					m.PaceTiZJSON = string(b)
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
	m.IsEstimated = false
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
	return &metricsDTO{
		AvgHr: m.AvgHr, Trimp: m.Trimp, Trimp100: m.Trimp100,
		TrimpZonal: m.TrimpZonal, HrTiZ: tiz,
		Rtss: m.Rtss, PaceTiZ: ptiz, IsEstimated: m.IsEstimated,
	}, nil
}

// recomputeMetrics 对单个活动重算全部度量。
func recomputeMetrics(db *gorm.DB, athlete *store.AthleteModel, activity *store.ActivityModel) error {
	sm, err := store.GetStream(db, activity.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil // 无 Stream：估算属票 07
	}
	if err != nil {
		return err
	}
	stream, err := core.UnmarshalStream(sm.Data)
	if err != nil {
		return err
	}
	return computeMetrics(db, athlete, activity, stream)
}
