package store

// ActivityMetricsModel 活动度量快照（票 05 心率链 / 06 配速链 / 09 功率链）。
type ActivityMetricsModel struct {
	ActivityID   uint    `gorm:"primaryKey" json:"activityId"`
	AvgHr        float64 `json:"avgHr"`
	Trimp        float64 `json:"trimp"`
	Trimp100     float64 `json:"trimp100"`
	TrimpZonal   float64 `json:"trimpZonal"`
	Rtss         float64 `json:"rtss"`
	Np           float64 `json:"np"`        // 归一化功率（4 次方均值，票 09）
	PowerLoad    float64 `json:"powerLoad"` // 类 BikeScore：100·hours·(NP/CP)²
	HrTiZJSON    string  `gorm:"column:hr_tiz" json:"-"`    // [5]float64 JSON
	PaceTiZJSON  string  `gorm:"column:pace_tiz" json:"-"`  // [6]float64 JSON
	PowerTiZJSON string  `gorm:"column:power_tiz" json:"-"` // [6]float64 JSON
	IsEstimated  bool    `json:"isEstimated"`
}

// TableName 显式表名。
func (ActivityMetricsModel) TableName() string { return "activity_metrics" }
