package store

// GORM 模型：数据库表形状。领域类型在 core/，这里只负责持久化与映射。
// json tag 是对外的 API 契约（小驼峰），不得依赖 Go 默认字段名。

// UserModel 管理员账号（票 02；单管理员）。PasswordHash 永不出 API。
type UserModel struct {
	ID           uint   `gorm:"primaryKey" json:"id"`
	Username     string `gorm:"uniqueIndex;size:64" json:"username"`
	PasswordHash string `gorm:"size:128" json:"-"`
}

// SecretModel 应用级密钥（如 JWT 签名密钥），重启后 token 仍有效。永不出 API。
type SecretModel struct {
	// 列名用 secret_key：key 是 MySQL 保留字（SQLite 放行、MySQL 报 1064）
	Key   string `gorm:"primaryKey;size:64;column:secret_key" json:"-"`
	Value string `gorm:"size:256" json:"-"`
}

// AthleteModel 运动员档案（见 CONTEXT.md 的 Athlete）。Sex 用于 TRIMP 的 Ksex。
type AthleteModel struct {
	ID   uint   `gorm:"primaryKey" json:"id"`
	Name string `gorm:"size:128;not null" json:"name"`
	Sex  string `gorm:"size:1;default:M" json:"sex"` // M / F（TRIMP Ksex）
	// AvatarURL / Bio 是主页资料卡的可选展示信息（留空则用首字母头像）。
	AvatarURL string `gorm:"size:512" json:"avatarUrl"`
	Bio       string `gorm:"size:280" json:"bio"`
	// 跑量目标（公里；0 = 未设置，首页不显示进度条）。
	YearlyGoalKm  int   `json:"yearlyGoalKm"`
	MonthlyGoalKm int   `json:"monthlyGoalKm"`
	WeeklyGoalKm  int   `json:"weeklyGoalKm"`
	CreatedAt     int64 `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt     int64 `gorm:"autoUpdateTime" json:"updatedAt"`
}

// TableName 显式表名（与 spec 数据模型词汇一致；GORM 默认复数化会得到 user_models 等）。
func (UserModel) TableName() string { return "users" }

func (SecretModel) TableName() string { return "secrets" }

func (AthleteModel) TableName() string { return "athletes" }
