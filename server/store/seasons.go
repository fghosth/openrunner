package store

import (
	"time"

	"gorm.io/gorm"
)

// UpsertSeason 创建/更新赛季。
func UpsertSeason(db *gorm.DB, s *SeasonModel) error {
	if s.AthleteID == 0 {
		return &ValidationError{"缺少档案"}
	}
	if s.Name == "" {
		return &ValidationError{"赛季名称不能为空"}
	}
	if err := validateDateRange(s.StartDate, s.EndDate); err != nil {
		return err
	}
	if s.Seed < 0 {
		return &ValidationError{"播种值不能为负"}
	}
	if s.ID == 0 {
		return db.Create(s).Error
	}
	res := db.Model(&SeasonModel{}).Where("id = ? AND athlete_id = ?", s.ID, s.AthleteID).
		Updates(map[string]any{
			"name": s.Name, "start_date": s.StartDate, "end_date": s.EndDate, "seed": s.Seed,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ListSeasons 列出档案赛季（按开始日期）。
func ListSeasons(db *gorm.DB, athleteID uint) ([]SeasonModel, error) {
	var out []SeasonModel
	if err := db.Where("athlete_id = ?", athleteID).Order("start_date").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteSeason 删除赛季。
func DeleteSeason(db *gorm.DB, athleteID, id uint) error {
	res := db.Where("athlete_id = ?", athleteID).Delete(&SeasonModel{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

var _ = time.Now
