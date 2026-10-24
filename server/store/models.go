package store

// GORM 模型：数据库表形状。领域类型在 openrunner-core，这里只负责持久化与映射。
// json tag 是对外的 API 契约（小驼峰），不得依赖 Go 默认字段名。

// UserModel 管理员账号（票 02；单管理员）。PasswordHash 永不出 API。
type UserModel struct {
	ID           uint   `gorm:"primaryKey" json:"id"`
	Username     string `gorm:"uniqueIndex;size:64" json:"username"`
	PasswordHash string `gorm:"size:128" json:"-"`
}

// SecretModel 应用级密钥（如 JWT 签名密钥），重启后 token 仍有效。永不出 API。
type SecretModel struct {
	Key   string `gorm:"primaryKey;size:64" json:"-"`
	Value string `gorm:"size:256" json:"-"`
}

// AthleteModel 运动员档案（见 CONTEXT.md 的 Athlete）。
type AthleteModel struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	Name      string `gorm:"size:128;not null" json:"name"`
	CreatedAt int64  `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt int64  `gorm:"autoUpdateTime" json:"updatedAt"`
}
