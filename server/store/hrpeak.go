package store

import (
	"encoding/json"
	"time"

	"github.com/fghosth/openrunner/core"
	"gorm.io/gorm"
)

// observedWindowSecs 是「持续峰值」的窗口长度。30 秒足以滤掉光电腕表的孤立尖峰，
// 又不会短到把真实的间歇冲刺峰值抹平。
const observedWindowSecs = 30

// ObservedHr 是从历史活动数据反推出来的心率观测结果。
type ObservedHr struct {
	// MaxHr 是推荐填入的观测最大心率（bpm）= 数据质量达标活动中的单点峰值。
	// 没有任何心率数据时为 0。
	MaxHr float64 `json:"maxHr"`
	// SustainedHr 是持续 30 秒的最高均值 —— 比 MaxHr 保守，用作可信度参照。
	SustainedHr float64 `json:"sustainedHr"`
	// Samples 是参与统计的有效心率样本总数。
	Samples int `json:"samples"`
	// Activities 是有心率数据、被扫描过的活动数。
	Activities int `json:"activities"`
	// Excluded 是因数据质量不足而被剔除的活动数。
	Excluded int `json:"excluded"`
	// PeakActivityID / PeakActivityAt 指向观测到持续峰值的活动（无则零值）。
	PeakActivityID uint       `json:"peakActivityId"`
	PeakActivityAt *time.Time `json:"peakActivityAt,omitempty"`
}

// ObservedMaxHeartRate 扫描该 Athlete 全部活动的逐秒心率通道，返回观测最大心率。
//
// 用途：用户建档时往往不知道自己的真实 MaxHR。与其让用户猜，不如用已有数据反推
// —— 只要有过一次全力冲刺/爬坡，观测峰值就非常接近真实 MaxHR。
// 注意它只是「观测下界」：若无高强度活动，真实 MaxHR 会高于此值。
func ObservedMaxHeartRate(db *gorm.DB, athleteID uint) (ObservedHr, error) {
	type row struct {
		ActivityID uint
		StartedAt  int64
		Data       []byte
	}
	var rows []row
	err := db.Table("activity_streams").
		Select("activity_streams.activity_id, activities.start_time AS started_at, activity_streams.data").
		Joins("JOIN activities ON activities.id = activity_streams.activity_id").
		Where("activities.athlete_id = ?", athleteID).
		Scan(&rows).Error
	if err != nil {
		return ObservedHr{}, err
	}

	out := ObservedHr{}
	for _, r := range rows {
		if len(r.Data) == 0 {
			continue
		}
		var stream core.Stream
		if err := json.Unmarshal(r.Data, &stream); err != nil {
			continue // 脏数据跳过，不因单条损坏让整个统计失败
		}
		obs := core.ObserveHeartRate(stream.HeartRate, stream.RecIntervalSecs, observedWindowSecs)
		if obs.Samples == 0 {
			continue
		}
		// 数据质量不达标的活动（多为力量训练/球类时的光电伪影）不参与推荐值，
		// 但它的单点峰值仍计入 PeakHr 供参考。
		if obs.Quality < core.MinQualityForPeak() {
			// 低质量活动的峰值不进 PeakHr：它们几乎全是伪影（实测 207 就来自
			// 一次 0.2km 的「other」活动，有效率仅 30%）。
			out.Excluded++
			continue
		}
		out.Activities++
		out.Samples += obs.Samples
		if obs.Sustained > out.SustainedHr {
			out.SustainedHr = obs.Sustained
		}
		if obs.Peak > out.MaxHr {
			out.MaxHr = obs.Peak
			out.PeakActivityID = r.ActivityID
			t := time.Unix(r.StartedAt, 0).Local()
			out.PeakActivityAt = &t
		}
	}
	return out, nil
}
