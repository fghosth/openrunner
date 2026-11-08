package store

import "gorm.io/gorm"

// PaceZoneModel 配速区间参数（票 06 v0：阈值配速按日期区间生效）。
// ThresholdPaceSecsPerKm：阈值配速，秒/公里（跑者习惯口径，UI 提供 mm:ss 输入）。
type PaceZoneModel struct {
	ID                     uint   `gorm:"primaryKey" json:"id"`
	AthleteID              uint   `gorm:"index" json:"athleteId"`
	ValidFrom              string `gorm:"size:10" json:"validFrom"`
	ValidTo                string `gorm:"size:10" json:"validTo"`
	ThresholdPaceSecsPerKm int    `json:"thresholdPaceSecsPerKm"`
}

// TableName 显式表名。
func (PaceZoneModel) TableName() string { return "pace_zones" }

// DeletePaceZone 按档案删除一条阈值配速记录。
func DeletePaceZone(db *gorm.DB, athleteID, id uint) error {
	res := db.Where("athlete_id = ? AND id = ?", athleteID, id).Delete(&PaceZoneModel{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrZoneNotFound
	}
	return nil
}
