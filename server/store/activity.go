package store

// ActivityModel 一次运动的持久化形状（见 CONTEXT.md 的 Activity）。
type ActivityModel struct {
	ID             uint    `gorm:"primaryKey" json:"id"`
	AthleteID      uint    `gorm:"index;not null" json:"athleteId"`
	Sport          string  `gorm:"size:16;not null" json:"sport"`
	StartTime      int64   `gorm:"not null" json:"startTime"` // unix 秒（绝对时刻；展示层按浏览器本地时区渲染）
	DurationSecs   int     `json:"durationSecs"`              // 含停表段（elapsed）
	TimerSecs      int     `json:"timerSecs"`                 // 不含停表段（timer）；负荷指标优先
	DistanceM      float64 `json:"distanceM"`
	ElevationGainM float64 `json:"elevationGainM"`
	IsPlanned      bool    `json:"isPlanned"`
	HasStream      bool    `json:"hasStream"`
	Source         string  `gorm:"size:32" json:"source"` // import_fit / 平台名
	ContentHash    string  `gorm:"size:64" json:"-"`      // 文件内容指纹（去重；唯一性由 partial index 保证）
}

// ActivityStreamModel 逐秒序列 BLOB（json 序列化的 core.Stream），与 Activity 一对一。
type ActivityStreamModel struct {
	ActivityID      uint   `gorm:"primaryKey" json:"activityId"`
	RecIntervalSecs int    `json:"recIntervalSecs"`
	Data            []byte `gorm:"type:blob" json:"-"`
}

// TableName 显式指定表名（GORM 默认会给 Model 后缀复数化，与 spec 词汇不符）。
func (ActivityModel) TableName() string { return "activities" }

func (ActivityStreamModel) TableName() string { return "activity_streams" }
