package auth

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/utils"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog/log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var jwtSecret []byte

const secretFileName = ".jwt_secret"
const secretDirPath = "./data/"

func init() {
	fullPath := filepath.Join(secretDirPath, secretFileName)
	secret, err := os.ReadFile(fullPath)
	if err == nil && len(secret) >= 32 {
		jwtSecret = secret
		log.Info().Str("path", fullPath).Msg("已从文件加载 JWT 密钥")
		return
	}
	log.Warn().Str("path", fullPath).Msg("JWT 密钥文件不存在或无效，正在生成新的密钥...")
	newSecret := make([]byte, 32)
	if _, err := rand.Read(newSecret); err != nil {
		log.Fatal().Err(err).Msg("无法生成新的 JWT 密钥")
	}
	if err := os.MkdirAll(secretDirPath, 0o750); err != nil {
		log.Fatal().Err(err).Msg("无法创建用于存储密钥的目录")
	}
	if err := os.WriteFile(fullPath, newSecret, 0o600); err != nil {
		log.Fatal().Err(err).Msg("无法保存新的 JWT 密钥到文件")
	}
	jwtSecret = newSecret
	log.Info().Str("path", fullPath).Msg("已成功生成并保存新的 JWT 密钥")
}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func LoginHandler(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户名或密码不能为空"})
		return
	}
	var user models.User
	if err := database.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	matched, needsUpgrade, newHash := utils.CheckAndUpgradePassword(req.Password, user.PasswordHash)
	if !matched {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	if needsUpgrade && newHash != "" {
		database.DB.Model(&user).Updates(map[string]interface{}{
			"password_hash":    newHash,
			"password_version": 1,
		})
		log.Info().Str("username", user.Username).Msg("密码存储已自动升级")
	}

	tokenString, err := generateToken(user.Username, user.TokenVersion)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法生成 Token"})
		return
	}
	c.SetSameSite(http.SameSiteStrictMode)
	secure := c.Request.TLS != nil
	c.SetCookie("cloudstream_token", tokenString, 7*24*3600, "/", "", secure, true)
	c.JSON(http.StatusOK, gin.H{
		"code":                  0,
		"needsPasswordReminder": user.NeedsPasswordReminder,
		"passwordReminderShown": user.PasswordReminderShown,
	})
}

func generateToken(username string, tokenVersion int) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"username": username,
		"version":  tokenVersion,
		"exp":      time.Now().Add(7 * 24 * time.Hour).Unix(),
		"iat":      time.Now().Unix(),
	})
	return token.SignedString(jwtSecret)
}

func JWTAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		var tokenString string
		if authHeader != "" {
			fmt.Sscanf(authHeader, "Bearer %s", &tokenString)
		}
		if tokenString == "" {
			if cookieToken, err := c.Cookie("cloudstream_token"); err == nil {
				tokenString = cookieToken
			}
		}
		if tokenString == "" {
			tokenString = c.Query("token")
		}
		if tokenString == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "请求未包含 Token"})
			return
		}
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("非预期的签名方法: %v", token.Header["alg"])
			}
			return jwtSecret, nil
		})
		if err != nil {
			log.Warn().Err(err).Msg("JWT 验证失败")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token 无效或已过期"})
			return
		}
		if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
			username, _ := claims["username"].(string)
			version, _ := claims["version"].(float64)
			var user models.User
			if err := database.DB.Where("username = ?", username).First(&user).Error; err != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token 对应的用户不存在"})
				return
			}
			if int(version) != user.TokenVersion {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token 已失效，请重新登录"})
				return
			}
			c.Set("username", username)
			c.Next()
		} else {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token 无效"})
		}
	}
}

func SignStreamURL(taskID uint, accountID uint, realIdentity string, expireHours int) (string, error) {
	if len(jwtSecret) == 0 {
		return "", fmt.Errorf("secret not initialized")
	}

	taskStr := strconv.FormatUint(uint64(taskID), 10)
	taskB64 := base64.RawURLEncoding.EncodeToString([]byte(taskStr))

	accStr := strconv.FormatUint(uint64(accountID), 10)
	accB64 := base64.RawURLEncoding.EncodeToString([]byte(accStr))

	var expiry int64
	if expireHours == 0 {
		expiry = 0 // 0 = 永不过期，与 OpenList 默认行为一致
	} else {
		expiry = time.Now().Add(time.Duration(expireHours) * time.Hour).Unix()
	}
	expStr := strconv.FormatInt(expiry, 10)

	realIDB64 := base64.RawURLEncoding.EncodeToString([]byte(realIdentity))

	salt := make([]byte, 8)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	saltB64 := base64.RawURLEncoding.EncodeToString(salt)

	payload := fmt.Sprintf("%d:%d:%d:%s:%s", taskID, accountID, expiry, realIdentity, saltB64)
	mac := hmac.New(sha256.New, jwtSecret)
	mac.Write([]byte(payload))
	sigHex := hex.EncodeToString(mac.Sum(nil))

	return fmt.Sprintf("%s:%s:%s:%s:%s:%s", taskB64, accB64, expStr, sigHex, realIDB64, saltB64), nil
}

func VerifyStreamSign(signStr string) (uint, uint, string, error) {
	parts := strings.Split(signStr, ":")
	if len(parts) != 6 {
		return 0, 0, "", fmt.Errorf("invalid sign format")
	}

	taskB64, accB64, expStr, sigHex, realIDB64, saltB64 := parts[0], parts[1], parts[2], parts[3], parts[4], parts[5]

	taskBytes, err := base64.RawURLEncoding.DecodeString(taskB64)
	if err != nil {
		return 0, 0, "", fmt.Errorf("invalid task encoding")
	}
	taskID, err := strconv.ParseUint(string(taskBytes), 10, 32)
	if err != nil {
		return 0, 0, "", fmt.Errorf("invalid task id")
	}

	accBytes, err := base64.RawURLEncoding.DecodeString(accB64)
	if err != nil {
		return 0, 0, "", fmt.Errorf("invalid account encoding")
	}
	accID, err := strconv.ParseUint(string(accBytes), 10, 32)
	if err != nil {
		return 0, 0, "", fmt.Errorf("invalid account id")
	}

	expiry, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return 0, 0, "", fmt.Errorf("invalid expiry")
	}
	if expiry != 0 && time.Now().Unix() > expiry {
		return 0, 0, "", fmt.Errorf("link expired")
	}

	realBytes, err := base64.RawURLEncoding.DecodeString(realIDB64)
	if err != nil {
		return 0, 0, "", fmt.Errorf("invalid real identity encoding")
	}
	realIdentity := string(realBytes)

	payload := fmt.Sprintf("%d:%d:%d:%s:%s", taskID, accID, expiry, realIdentity, saltB64)
	mac := hmac.New(sha256.New, jwtSecret)
	mac.Write([]byte(payload))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expectedSig), []byte(sigHex)) {
		return 0, 0, "", fmt.Errorf("signature mismatch")
	}

	return uint(taskID), uint(accID), realIdentity, nil
}
