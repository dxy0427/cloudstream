package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"

	"golang.org/x/crypto/bcrypt"
)

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// IsSHA256Hex 判断字符串是否为 SHA-256 哈希（64位十六进制）
func IsSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// SHA256Hex 计算字符串的 SHA-256 哈希
func SHA256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func GenerateRandomPassword() (string, error) {
	randomBytes := make([]byte, 24)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
}
