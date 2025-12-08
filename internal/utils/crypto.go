package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"

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

func DeriveAESKey(jwtSecret []byte) []byte {
	hash := sha256.Sum256(jwtSecret)
	return hash[:]
}

func EncryptClientSecret(secret string, jwtSecret []byte) (string, error) {
	block, err := aes.NewCipher(DeriveAESKey(jwtSecret))
	if err != nil {
		return "", fmt.Errorf("创建AES加密块失败: %w", err)
	}
	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", fmt.Errorf("生成IV失败: %w", err)
	}
	stream := cipher.NewCFBEncrypter(block, iv)
	encrypted := make([]byte, len(secret))
	stream.XORKeyStream(encrypted, []byte(secret))
	return base64.StdEncoding.EncodeToString(append(iv, encrypted...)), nil
}

func DecryptClientSecret(encryptedSecret string, jwtSecret []byte) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encryptedSecret)
	if err != nil {
		return "", fmt.Errorf("Base64解码失败: %w", err)
	}
	if len(data) < aes.BlockSize {
		return "", fmt.Errorf("加密数据无效，长度不足")
	}
	iv := data[:aes.BlockSize]
	encrypted := data[aes.BlockSize:]
	block, err := aes.NewCipher(DeriveAESKey(jwtSecret))
	if err != nil {
		return "", fmt.Errorf("创建AES解密块失败: %w", err)
	}
	stream := cipher.NewCFBDecrypter(block, iv)
	decrypted := make([]byte, len(encrypted))
	stream.XORKeyStream(decrypted, encrypted)
	return string(decrypted), nil
}