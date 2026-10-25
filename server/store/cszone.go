package store

// CsZoneModel 临界域参数（票 08/09：按日期区间生效）。
// Domain 区分两个物理域：speed（CS m/s + D' m，配速链）与 power（CP W + W' J，功率链）——
// 同表不同域，Round 7 决议（此前同字段双单位属语义错配）。
type CsZoneModel struct {
	ID        uint    `gorm:"primaryKey" json:"id"`
	AthleteID uint    `gorm:"index" json:"athleteId"`
	Domain    string  `gorm:"size:8;index" json:"domain"` // speed / power
	ValidFrom string  `gorm:"size:10" json:"validFrom"`
	ValidTo   string  `gorm:"size:10" json:"validTo"`
	Cs        float64 `json:"cs"`                    // speed 域: m/s | power 域: CP, W
	Dprime    float64 `json:"dprime"`                // speed 域: m   | power 域: W', J
	Source    string  `gorm:"size:16" json:"source"` // manual / fit
}

// TableName 显式表名。
func (CsZoneModel) TableName() string { return "cs_zones" }
