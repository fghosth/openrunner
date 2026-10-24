package store

// ActivityMetricsModel 活动度量快照（票 05 起：心率链；后续票扩展列）。
type ActivityMetricsModel struct {
	ActivityID  uint    `gorm:"primaryKey" json:"activityId"`
	AvgHr       float64 `json:"avgHr"`
	Trimp       float64 `json:"trimp"`
	Trimp100    float64 `json:"trimp100"`
	TrimpZonal  float64 `json:"trimpZonal"`
	HrTiZJSON   string  `gorm:"column:hr_tiz" json:"-"` // [5]float64 JSON
	IsEstimated bool    `json:"isEstimated"`
}

// TableName 显式表名。
func (ActivityMetricsModel) TableName() string { return "activity_metrics" }
