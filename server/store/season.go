package store

// SeasonModel 训练赛季（PMC 播种与规划单位）。
type SeasonModel struct {
	ID        uint    `gorm:"primaryKey" json:"id"`
	AthleteID uint    `gorm:"index" json:"athleteId"`
	Name      string  `gorm:"size:128;not null" json:"name"`
	StartDate string  `gorm:"size:10;not null" json:"startDate"`
	EndDate   string  `gorm:"size:10;not null" json:"endDate"`
	Seed      float64 `json:"seed"` // 赛季起点 LTS/STS 初值（0=不播种）
}

// TableName 显式表名。
func (SeasonModel) TableName() string { return "seasons" }
