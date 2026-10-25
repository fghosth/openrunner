package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

// 凭证加密（票 15）：AES-256-GCM，密钥从 secrets 表的 jwt_secret 派生（HKDF 简化版）。
// 与 JWT 复用同一根密钥但经不同 label 派生，避免密钥复用风险。

// deriveCredentialKey 从根密钥派生凭证加密密钥（标签隔离）。
func deriveCredentialKey(root []byte) []byte {
	h := sha256.New()
	h.Write(root)
	h.Write([]byte("openrunner/credentials/v1"))
	return h.Sum(nil)
}

// EncryptCredential 加密平台凭证；返回 nonce||ciphertext。
func EncryptCredential(root, plaintext []byte) ([]byte, error) {
	key := deriveCredentialKey(root)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// DecryptCredential 解密平台凭证。
func DecryptCredential(root, ciphertext []byte) ([]byte, error) {
	key := deriveCredentialKey(root)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, fmt.Errorf("密文长度不足")
	}
	nonce, ct := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}

var _ = hex.EncodeToString
