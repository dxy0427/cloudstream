package utils

import (
	"crypto/sha256"
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

// CheckPasswordWithUpgradeV2 支持双字段的密码校验
// password: 前端发来的值（SHA-256 哈希）
// passwordPlain: 原始明文（新前端额外提供，用于旧方案fallback）
// storedHash: 数据库中存储的 bcrypt 哈希
// 返回 (是否匹配, 是否需要升级, 升级用的新哈希值)
func CheckPasswordWithUpgradeV2(password, passwordPlain, storedHash string) (bool, bool, string) {
	if IsSHA256Hex(password) {
		// 新方案：password 是 SHA-256 哈希
		if CheckPasswordHash(password, storedHash) {
			return true, false, "" // 新方案匹配
		}
		// 旧方案 fallback：用原始明文尝试
		if passwordPlain != "" && CheckPasswordHash(passwordPlain, storedHash) {
			// 旧方案匹配，升级为 bcrypt(SHA256(plaintext))
			newHash, err := HashPassword(password)
			if err != nil {
				return true, true, ""
			}
			return true, true, newHash
		}
		return false, false, ""
	}

	// 旧前端（传明文）
	if CheckPasswordHash(password, storedHash) {
		return true, true, ""
	}
	return false, false, ""
}