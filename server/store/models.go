package store

// GORM 模型：数据库表形状。领域类型在 openrunner-core，这里只负责持久化与映射。

// UserModel 管理员账号（票 02；单管理员）。
type UserModel struct {
	ID           uint   `gorm:"primaryKey"`
	Username     string `gorm:"uniqueIndex;size:64"`
	PasswordHash string `gorm:"size:128"`
}

// SecretModel 应用级密钥（如 JWT 签名密钥），重启后 token 仍有效。
type SecretModel struct {
	Key   string `gorm:"primaryKey;size:64"`
	Value string `gorm:"size:256"`
}

// AthleteModel 运动员档案（见 CONTEXT.md 的 Athlete）。
type AthleteModel struct {
	ID        uint   `gorm:"primaryKey"`
	Name      string `gorm:"size:128;not null"`
	CreatedAt int64
	UpdatedAt int64
}
