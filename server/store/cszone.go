package store

// CsZoneModel 临界速度参数（票 08：手动覆盖或自动拟合结果，按日期区间生效）。
type CsZoneModel struct {
	ID        uint    `gorm:"primaryKey" json:"id"`
	AthleteID uint    `gorm:"index" json:"athleteId"`
	ValidFrom string  `gorm:"size:10" json:"validFrom"`
	ValidTo   string  `gorm:"size:10" json:"validTo"`
	Cs        float64 `json:"cs"`                    // m/s
	Dprime    float64 `json:"dprime"`                // m
	Source    string  `gorm:"size:16" json:"source"` // manual / fit
}

// TableName 显式表名。
func (CsZoneModel) TableName() string { return "cs_zones" }
