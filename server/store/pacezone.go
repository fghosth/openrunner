package store

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
