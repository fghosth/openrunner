package store

import (
	"encoding/json"
	"time"

	"github.com/openrunner/core"
	"gorm.io/gorm"
)

// ObservedHr 是从历史活动数据反推出来的心率观测结果。
type ObservedHr struct {
	// MaxHr 是观测到的最大心率（bpm）；没有任何心率数据时为 0。
	MaxHr float64 `json:"maxHr"`
	// Samples 是参与统计的有效心率样本总数。
	Samples int `json:"samples"`
	// Activities 是有心率数据、被扫描过的活动数。
	Activities int `json:"activities"`
	// PeakActivityID / PeakActivityAt 指向观测到峰值的活动（无则零值）。
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
		peak := core.PeakHeartRate(stream.HeartRate)
		if peak.Samples == 0 {
			continue
		}
		out.Activities++
		out.Samples += peak.Samples
		if peak.MaxHr > out.MaxHr {
			out.MaxHr = peak.MaxHr
			out.PeakActivityID = r.ActivityID
			t := time.Unix(r.StartedAt, 0).Local()
			out.PeakActivityAt = &t
		}
	}
	return out, nil
}
