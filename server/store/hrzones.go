package store

import (
	"errors"
	"fmt"
	"time"

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
	if err := validateDateRange(z.ValidFrom, z.ValidTo); err != nil {
		return err
	}
	if z.ID == 0 {
		return db.Create(z).Error
	}
	res := db.Model(&HrZoneModel{}).Where("id = ? AND athlete_id = ?", z.ID, z.AthleteID).
		Updates(map[string]any{
			"valid_from": z.ValidFrom, "valid_to": z.ValidTo,
			"max_hr": z.MaxHr, "rest_hr": z.RestHr, "lt_hr": z.LtHr,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrHrZoneNotFound
	}
	return nil
}

// validateDateRange 严格校验 YYYY-MM-DD（仅 len==10 会让 "abcd-efgh" 混入字典序匹配）。
func validateDateRange(from, to string) error {
	if _, err := time.Parse("2006-01-02", from); err != nil {
		return &ValidationError{"起始日期无效（需 YYYY-MM-DD）"}
	}
	if _, err := time.Parse("2006-01-02", to); err != nil {
		return &ValidationError{"结束日期无效（需 YYYY-MM-DD）"}
	}
	if from > to {
		return &ValidationError{"起始日期晚于结束日期"}
	}
	return nil
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
	// 重叠区间的取用规则：生效起点最晚者优先，同起点取后建者（id DESC）
	err := db.Where("athlete_id = ? AND valid_from <= ? AND valid_to >= ?", athleteID, date, date).
		Order("valid_from DESC, id DESC").First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrHrZoneNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}
