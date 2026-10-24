package api

import (
	"encoding/json"
	"errors"

	"github.com/openrunner/core"
	"github.com/openrunner/server/store"
	"gorm.io/gorm"
)

// metricsDTO 心率链度量（票 05）。
type metricsDTO struct {
	AvgHr       float64   `json:"avgHr"`
	Trimp       float64   `json:"trimp"`
	Trimp100    float64   `json:"trimp100"`
	TrimpZonal  float64   `json:"trimpZonal"`
	HrTiZ       []float64 `json:"hrTiZ"`
	IsEstimated bool      `json:"isEstimated"`
}

// computeHrMetrics 依据活动日期生效的心率区间与档案性别计算心率链指标并落库。
// 无覆盖区间时不写 metrics（detail 显示为 null，UI 引导配置）。
func computeHrMetrics(db *gorm.DB, athlete *store.AthleteModel, activity *store.ActivityModel, stream core.Stream) error {
	date := timeToDateString(activity.StartTime)
	zone, err := store.HrZoneForDate(db, athlete.ID, date)
	if errors.Is(err, store.ErrHrZoneNotFound) {
		return nil // 无配置 → 无 metrics
	}
	if err != nil {
		return err
	}

	avgHr := stream.AvgHeartRate()
	if avgHr <= 0 {
		return nil // 无心率数据
	}

	ksex := 1.92
	if athlete.Sex == "F" {
		ksex = 1.67
	}
	durationSecs := float64(activity.TimerSecs)
	if durationSecs <= 0 {
		durationSecs = float64(activity.DurationSecs) // 回退（手工活动可能无 Timer）
	}

	trimp := core.TrimpPoints(durationSecs, avgHr, float64(zone.MaxHr), float64(zone.RestHr), ksex)
	trimp100 := core.Trimp100(trimp, float64(zone.MaxHr), float64(zone.RestHr), float64(zone.LtHr), ksex)
	zonal := core.TrimpZonal(stream.HeartRate, float64(zone.MaxHr), stream.RecIntervalSecs)
	tiz := core.HrTimeInZone(stream.HeartRate, float64(zone.MaxHr), stream.RecIntervalSecs)
	tizJSON, err := json.Marshal(tiz)
	if err != nil {
		return err
	}

	m := store.ActivityMetricsModel{
		ActivityID: activity.ID, AvgHr: avgHr,
		Trimp: trimp, Trimp100: trimp100, TrimpZonal: zonal,
		HrTiZJSON: string(tizJSON), IsEstimated: false,
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
	return &metricsDTO{
		AvgHr: m.AvgHr, Trimp: m.Trimp, Trimp100: m.Trimp100,
		TrimpZonal: m.TrimpZonal, HrTiZ: tiz, IsEstimated: m.IsEstimated,
	}, nil
}

// recomputeHrMetrics 对单个活动重算心率链指标。
func recomputeHrMetrics(db *gorm.DB, athlete *store.AthleteModel, activity *store.ActivityModel) error {
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
	return computeHrMetrics(db, athlete, activity, stream)
}
