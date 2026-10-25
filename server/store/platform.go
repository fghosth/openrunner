package store

// PlatformConnectionModel 云平台连接（票 15）：凭证加密存储 + 同步游标。
type PlatformConnectionModel struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	AthleteID uint   `gorm:"index" json:"athleteId"`
	Platform  string `gorm:"size:32;index" json:"platform"` // garmin / ...
	// CredentialEnc AES-GCM 加密后的平台凭证（Garmin Personal API Key）。
	// 永不出 API（json:"-"）。
	CredentialEnc []byte `gorm:"type:longblob" json:"-"`
	// SyncCursor 最近一次成功同步的水位（"2026-05-01"，增量拉取用）。
	SyncCursor string `gorm:"size:10" json:"syncCursor"`
	LastSyncAt int64  `json:"lastSyncAt"`
	LastError  string `gorm:"size:512" json:"lastError"`
	Enabled    bool   `json:"enabled"`
}

// TableName 显式表名。
func (PlatformConnectionModel) TableName() string { return "platform_connections" }

// SyncRunModel 同步运行记录（票 15：同步过程状态反馈）。
type SyncRunModel struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	AthleteID  uint   `gorm:"index" json:"athleteId"`
	Platform   string `gorm:"size:32" json:"platform"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt int64  `json:"finishedAt"`
	Imported   int    `json:"imported"`
	Skipped    int    `json:"skipped"`
	Failed     int    `json:"failed"`
	Status     string `gorm:"size:16" json:"status"` // running / ok / error
	Message    string `gorm:"size:512" json:"message"`
}

// TableName 显式表名。
func (SyncRunModel) TableName() string { return "sync_runs" }
