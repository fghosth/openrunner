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
	// PlanType 计划课型（easy/recovery/long/tempo/threshold/intervals/hills/race）。
	// 只有 isPlanned 的活动有语义；空串按 easy 处理（历史数据无此列）。
	PlanType  string  `gorm:"size:16" json:"planType"`
	AvgHr     float64 `json:"avgHr"` // summary-only 活动的均值心率（估算源）
	HasStream bool    `json:"hasStream"`
	// HasGps 表示 Stream 里有可用经纬度（可在地图上画出轨迹）。
	// 与 HasStream 区分：心率/配速流也算 Stream，但没有 GPS 就画不出轨迹，
	// UI 不应给出「在地图上看」的入口（否则是死链接）。
	HasGps bool   `json:"hasGps"`
	Source string `gorm:"size:32" json:"source"` // import_fit / 平台名
	// ContentHash 文件内容指纹（去重）。指针类型以支持 NULL——
	// 手工/计划活动无指纹，写 NULL 而非空串：MySQL 普通唯一索引允许多个 NULL，
	// 但空串会在同档案内撞 (athlete_id, '')（Round 8 BLOCK 项）。
	ContentHash *string `gorm:"size:64" json:"-"`
}

// ActivityStreamModel 逐秒序列 BLOB（json 序列化的 core.Stream），与 Activity 一对一。
type ActivityStreamModel struct {
	ActivityID      uint `gorm:"primaryKey" json:"activityId"`
	RecIntervalSecs int  `json:"recIntervalSecs"`
	// longblob：MySQL 的 blob 上限 64KB，1h@1s 多通道 JSON 远超（Error 1406）。
	// 注意：AutoMigrate 不改已有列类型（Round 8：旧库需手动 ALTER）。
	Data []byte `gorm:"type:longblob" json:"-"`
}

// TableName 显式指定表名（GORM 默认会给 Model 后缀复数化，与 spec 词汇不符）。
func (ActivityModel) TableName() string { return "activities" }

func (ActivityStreamModel) TableName() string { return "activity_streams" }
