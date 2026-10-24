package store

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// UpsertHrZone 创建/更新一组心率区间（同档案多组按日期区间并存）。
func UpsertHrZone(db *gorm.DB, z *HrZoneModel) error {
	if z.AthleteID == 0 {
		return fmt.Errorf("缺少档案")
	}
	if z.RestHr <= 0 || z.MaxHr <= 0 || z.LtHr <= 0 {
		return &ValidationError{"心率数值必须为正"}
	}
	if z.RestHr >= z.MaxHr || z.LtHr > z.MaxHr || z.LtHr < z.RestHr {
		return &ValidationError{"需满足 RestHR < LTHR ≤ MaxHR"}
	}
	if len(z.ValidFrom) != 10 || len(z.ValidTo) != 10 || z.ValidFrom > z.ValidTo {
		return &ValidationError{"日期区间无效（YYYY-MM-DD）"}
	}
	if z.ID == 0 {
		return db.Create(z).Error
	}
	return db.Model(&HrZoneModel{}).Where("id = ? AND athlete_id = ?", z.ID, z.AthleteID).
		Updates(map[string]any{
			"valid_from": z.ValidFrom, "valid_to": z.ValidTo,
			"max_hr": z.MaxHr, "rest_hr": z.RestHr, "lt_hr": z.LtHr,
		}).Error
}

// ListHrZones 列出档案全部心率区间。
func ListHrZones(db *gorm.DB, athleteID uint) ([]HrZoneModel, error) {
	var out []HrZoneModel
	if err := db.Where("athlete_id = ?", athleteID).Order("valid_from").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// ErrHrZoneNotFound 心率区间不存在。
var ErrHrZoneNotFound = errors.New("心率区间不存在")

// HrZoneForDate 返回覆盖指定日期（YYYY-MM-DD 字典序比较）的区间；无覆盖返回 ErrHrZoneNotFound。
func HrZoneForDate(db *gorm.DB, athleteID uint, date string) (*HrZoneModel, error) {
	var m HrZoneModel
	err := db.Where("athlete_id = ? AND valid_from <= ? AND valid_to >= ?", athleteID, date, date).
		Order("valid_from DESC").First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrHrZoneNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}
