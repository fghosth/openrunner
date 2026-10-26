package store

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// RaceResultModel 是一条比赛/计时成绩，作为 Banister 表现模型的观测点。
//
// 不限定必须是官方赛事：自己跑的 5K 全力、10K 计时同样有效 —— 那正是拟合
// 「训练负荷 → 成绩」关系最需要的样本。
type RaceResultModel struct {
	ID        uint    `gorm:"primaryKey" json:"id"`
	AthleteID uint    `gorm:"index;not null" json:"athleteId"`
	Date      string  `gorm:"size:10;not null" json:"date"` // YYYY-MM-DD
	DistanceM float64 `gorm:"not null" json:"distanceM"`
	TimeSecs  int     `gorm:"not null" json:"timeSecs"`
	Name      string  `gorm:"size:128" json:"name"`
	Note      string  `gorm:"size:280" json:"note"`
	CreatedAt int64   `gorm:"autoCreateTime" json:"createdAt"`
}

func (RaceResultModel) TableName() string { return "race_results" }

// Validate 校验一条成绩的合法性。
func (r *RaceResultModel) Validate() error {
	if _, err := time.Parse("2006-01-02", r.Date); err != nil {
		return &ValidationError{"日期无效（需 YYYY-MM-DD）"}
	}
	if r.DistanceM <= 0 {
		return &ValidationError{"距离必须为正"}
	}
	if r.TimeSecs <= 0 {
		return &ValidationError{"用时必须为正"}
	}
	// 换算成配速做粗筛：低于 2:00/km 或慢于 30:00/km 基本都是录错了
	paceSecsPerKm := float64(r.TimeSecs) / (r.DistanceM / 1000)
	if paceSecsPerKm < 120 {
		return &ValidationError{"配速快于 2:00/km，请检查距离或用时"}
	}
	if paceSecsPerKm > 1800 {
		return &ValidationError{"配速慢于 30:00/km，请检查距离或用时"}
	}
	return nil
}

// CreateRaceResult 新增比赛成绩。
func CreateRaceResult(db *gorm.DB, r *RaceResultModel) error {
	if r.AthleteID == 0 {
		return fmt.Errorf("缺少档案")
	}
	if err := r.Validate(); err != nil {
		return err
	}
	return db.Create(r).Error
}

// UpdateRaceResult 更新比赛成绩（只能改自己档案下的）。
func UpdateRaceResult(db *gorm.DB, r *RaceResultModel) error {
	if err := r.Validate(); err != nil {
		return err
	}
	res := db.Model(&RaceResultModel{}).Where("id = ? AND athlete_id = ?", r.ID, r.AthleteID).
		Updates(map[string]any{
			"date": r.Date, "distance_m": r.DistanceM, "time_secs": r.TimeSecs,
			"name": r.Name, "note": r.Note,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrRaceResultNotFound
	}
	return nil
}

// ErrRaceResultNotFound 比赛成绩不存在。
var ErrRaceResultNotFound = errors.New("比赛成绩不存在")

// DeleteRaceResult 删除比赛成绩。
func DeleteRaceResult(db *gorm.DB, athleteID, id uint) error {
	res := db.Where("id = ? AND athlete_id = ?", id, athleteID).Delete(&RaceResultModel{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrRaceResultNotFound
	}
	return nil
}

// ListRaceResults 列出档案的比赛成绩（按日期倒序）。
func ListRaceResults(db *gorm.DB, athleteID uint) ([]RaceResultModel, error) {
	var out []RaceResultModel
	if err := db.Where("athlete_id = ?", athleteID).Order("date DESC, id DESC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
