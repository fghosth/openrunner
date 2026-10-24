package store

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// UpsertPaceZone 创建/更新阈值配速区间。
func UpsertPaceZone(db *gorm.DB, z *PaceZoneModel) error {
	if z.AthleteID == 0 {
		return fmt.Errorf("缺少档案")
	}
	if z.ThresholdPaceSecsPerKm < 120 || z.ThresholdPaceSecsPerKm > 1200 {
		return &ValidationError{"阈值配速需在 2:00/km ~ 20:00/km 之间"}
	}
	if len(z.ValidFrom) != 10 || len(z.ValidTo) != 10 || z.ValidFrom > z.ValidTo {
		return &ValidationError{"日期区间无效（YYYY-MM-DD）"}
	}
	if z.ID == 0 {
		return db.Create(z).Error
	}
	return db.Model(&PaceZoneModel{}).Where("id = ? AND athlete_id = ?", z.ID, z.AthleteID).
		Updates(map[string]any{
			"valid_from": z.ValidFrom, "valid_to": z.ValidTo,
			"threshold_pace_secs_per_km": z.ThresholdPaceSecsPerKm,
		}).Error
}

// ListPaceZones 列出全部配速区间。
func ListPaceZones(db *gorm.DB, athleteID uint) ([]PaceZoneModel, error) {
	var out []PaceZoneModel
	if err := db.Where("athlete_id = ?", athleteID).Order("valid_from").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// ErrPaceZoneNotFound 配速区间不存在。
var ErrPaceZoneNotFound = errors.New("配速区间不存在")

// PaceZoneForDate 返回覆盖指定日期的阈值配速。
func PaceZoneForDate(db *gorm.DB, athleteID uint, date string) (*PaceZoneModel, error) {
	var m PaceZoneModel
	err := db.Where("athlete_id = ? AND valid_from <= ? AND valid_to >= ?", athleteID, date, date).
		Order("valid_from DESC").First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPaceZoneNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}
