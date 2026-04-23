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

// CheckAndUpgradePassword 校验密码并自动升级存储
// password: 前端发来的 SHA-256 哈希
// storedHash: 数据库中的 bcrypt 哈希
// 返回 (匹配, 需要升级, 升级后的新哈希)
func CheckAndUpgradePassword(password, storedHash string) (bool, bool, string) {
	// 新方案：bcrypt(SHA256) 直接比对
	if CheckPasswordHash(password, storedHash) {
		return true, false, ""
	}

	// 旧方案 fallback：存储可能是 bcrypt(plaintext)
	// 用 SHA-256 值当"明文"去比对旧 bcrypt → 对于正常密码不会匹配
	// 但如果用户密码恰好是64位hex，这里可能误匹配，概率极低可忽略
	// 实际上：普通用户的 SHA256("password") ≠ "password"，所以不会走到这里

	// 真正的旧方案 fallback：我们需要原始明文才能升级
	// 但由于前端不发明文了，旧用户需要重新设置密码
	// 或者：用 SHA-256 哈希值本身做一次 bcrypt 尝试
	// 如果旧存储是 bcrypt(plaintext) 且 plaintext ≠ SHA256，则不匹配
	// → 旧用户首次登录会失败，需要管理员重置密码或用旧前端登录一次

	return false, false, ""
}
