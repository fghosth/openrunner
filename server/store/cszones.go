package store

import (
	"errors"

	"gorm.io/gorm"
)

// UpsertCsZone 创建/更新临界速度区间。
func UpsertCsZone(db *gorm.DB, z *CsZoneModel) error {
	if z.AthleteID == 0 {
		return &ValidationError{"缺少档案"}
	}
	if z.Cs < 1 || z.Cs > 12 { // 1~12 m/s（3'20"~1'23" /km 之外不合理）
		return &ValidationError{"CS 需在 1~12 m/s 之间"}
	}
	if z.Dprime < 10 || z.Dprime > 2000 {
		return &ValidationError{"D' 需在 10~2000 m 之间"}
	}
	if err := validateDateRange(z.ValidFrom, z.ValidTo); err != nil {
		return err
	}
	if z.Source == "" {
		z.Source = "manual"
	}
	if z.ID == 0 {
		return db.Create(z).Error
	}
	res := db.Model(&CsZoneModel{}).Where("id = ? AND athlete_id = ?", z.ID, z.AthleteID).
		Updates(map[string]any{
			"valid_from": z.ValidFrom, "valid_to": z.ValidTo,
			"cs": z.Cs, "dprime": z.Dprime, "source": z.Source,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ListCsZones 列出临界速度区间。
func ListCsZones(db *gorm.DB, athleteID uint) ([]CsZoneModel, error) {
	var out []CsZoneModel
	if err := db.Where("athlete_id = ?", athleteID).Order("valid_from").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// ErrCsZoneNotFound 临界速度区间不存在。
var ErrCsZoneNotFound = errCsZoneNotFound{}

type errCsZoneNotFound struct{}

func (errCsZoneNotFound) Error() string { return "临界速度区间不存在" }

// CsZoneForDate 返回覆盖指定日期的临界速度区间。
func CsZoneForDate(db *gorm.DB, athleteID uint, date string) (*CsZoneModel, error) {
	var m CsZoneModel
	err := db.Where("athlete_id = ? AND valid_from <= ? AND valid_to >= ?", athleteID, date, date).
		Order("valid_from DESC, id DESC").First(&m).Error
	if isNotFound(err) {
		return nil, ErrCsZoneNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func isNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
