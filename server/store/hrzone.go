package store

// HrZoneModel 心率区间（按日期区间生效；RestHR < LTHR ≤ MaxHR）。
type HrZoneModel struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	AthleteID uint   `gorm:"index" json:"athleteId"`
	ValidFrom string `gorm:"size:10" json:"validFrom"` // YYYY-MM-DD（含）
	ValidTo   string `gorm:"size:10" json:"validTo"`   // YYYY-MM-DD（含）
	MaxHr     int    `json:"maxHr"`
	RestHr    int    `json:"restHr"`
	LtHr      int    `json:"ltHr"`
}

// TableName 显式表名（与 spec 词汇一致）。
func (HrZoneModel) TableName() string { return "hr_zones" }
