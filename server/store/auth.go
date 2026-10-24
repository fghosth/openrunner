package store

import (
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	secretKeyJWT   = "jwt_secret"
	tokenValidity  = 7 * 24 * time.Hour
	minPasswordLen = 6
)

// ErrUserExists 表示管理员已初始化。
var ErrUserExists = errors.New("管理员已存在")

// ValidationError 表示输入不满足领域规则（API 层映射为 400，不带内部细节）。
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

// dummyHash 用于用户不存在时的等时 bcrypt 比对，抹平时序差。
var dummyHash = []byte("$2a$10$kDbrc2MUrOqHpAs3Jx4zre1VSDcyes9tztPY0VOuL3ygKFbKljhtq")

// NeedsSetup 报告是否尚未创建管理员（首次启动流程）。
func NeedsSetup(db *gorm.DB) (bool, error) {
	var count int64
	if err := db.Model(&UserModel{}).Count(&count).Error; err != nil {
		return false, err
	}
	return count == 0, nil
}

// CreateUser 创建管理员账号（仅首次 setup 允许，重复创建返回 ErrUserExists）。
func CreateUser(db *gorm.DB, username, password string) error {
	if username == "" {
		return &ValidationError{"用户名不能为空"}
	}
	if len(password) < minPasswordLen {
		return &ValidationError{fmt.Sprintf("密码至少 %d 位", minPasswordLen)}
	}
	var count int64
	if err := db.Model(&UserModel{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrUserExists
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return db.Create(&UserModel{Username: username, PasswordHash: string(hash)}).Error
}

// Authenticate 校验账密；无论失败原因，对外统一为"账密错误"（防账号枚举）。
func Authenticate(db *gorm.DB, username, password string) error {
	var u UserModel
	err := db.Where("username = ?", username).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 等时防护：用户不存在也执行一次同成本 bcrypt 比对
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return errors.New("账密错误")
	}
	if err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return errors.New("账密错误")
	}
	return nil
}

// JWTSecret 读取（必要时生成）JWT 签名密钥。
func JWTSecret(db *gorm.DB) ([]byte, error) {
	var s SecretModel
	if err := db.Where("key = ?", secretKeyJWT).First(&s).Error; err != nil {
		return nil, err
	}
	return hex.DecodeString(s.Value)
}

// SignToken 签发 HS256 JWT，subject 为用户名。
func SignToken(secret []byte, username string, exp time.Time) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   username,
		ExpiresAt: jwt.NewNumericDate(exp),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
}

// IssueToken 按默认有效期签发登录 token。
// TODO(安全): 登录/setup 端点当前无速率限制（单用户自托管场景接受；多用户前须补）。
func IssueToken(db *gorm.DB, username string) (string, error) {
	secret, err := JWTSecret(db)
	if err != nil {
		return "", err
	}
	return SignToken(secret, username, time.Now().Add(tokenValidity))
}

// ParseToken 校验 token（签名 + 过期），返回用户名。
func ParseToken(db *gorm.DB, tokenStr string) (string, error) {
	secret, err := JWTSecret(db)
	if err != nil {
		return "", err
	}
	claims := &jwt.RegisteredClaims{}
	tok, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("意外签名算法 %v", t.Header["alg"])
		}
		return secret, nil
	})
	if err != nil || !tok.Valid {
		return "", fmt.Errorf("token 无效")
	}
	return claims.Subject, nil
}

// SignExpiredTokenForTest 仅供测试：用真实密钥签一个指定过期时间（可为过去）的 token。
func SignExpiredTokenForTest(db *DB, exp time.Time) (string, error) {
	secret, err := JWTSecret(db.gorm)
	if err != nil {
		return "", err
	}
	return SignToken(secret, "admin", exp)
}
