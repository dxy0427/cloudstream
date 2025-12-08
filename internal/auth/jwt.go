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
	if !utils.CheckPasswordHash(req.Password, user.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}
	tokenString, err := generateToken(user.Username, user.TokenVersion)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法生成 Token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": tokenString})
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
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "请求未包含 Token"})
			return
		}
		var tokenString string
		fmt.Sscanf(authHeader, "Bearer %s", &tokenString)
		if tokenString == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token 格式不正确"})
			return
		}
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("非预期的签名方法: %v", token.Header["alg"])
			}
			return jwtSecret, nil
		})
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token 无效或已过期: " + err.Error()})
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

// SignStreamURL 生成包含 随机盐值(Salt) 的签名
func SignStreamURL(accountID uint, realIdentity string) (string, error) {
	if len(jwtSecret) == 0 {
		return "", fmt.Errorf("secret not initialized")
	}

	// 1. AccountID
	accStr := strconv.FormatUint(uint64(accountID), 10)
	accB64 := base64.RawURLEncoding.EncodeToString([]byte(accStr))

	// 2. Expiry
	expiry := time.Now().Add(24 * time.Hour).Unix()
	expStr := strconv.FormatInt(expiry, 10)

	// 3. RealIdentity
	realIDB64 := base64.RawURLEncoding.EncodeToString([]byte(realIdentity))

	// 4. Salt (随机盐值，确保每次签名不同)
	salt := make([]byte, 8) // 8字节随机数
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	saltB64 := base64.RawURLEncoding.EncodeToString(salt)

	// 5. HMAC Payload
	payload := fmt.Sprintf("%d:%d:%s:%s", accountID, expiry, realIdentity, saltB64)
	mac := hmac.New(sha256.New, jwtSecret)
	mac.Write([]byte(payload))
	sigHex := hex.EncodeToString(mac.Sum(nil))

	// 返回 5段式 字符串
	return fmt.Sprintf("%s:%s:%s:%s:%s", accB64, expStr, sigHex, realIDB64, saltB64), nil
}

// VerifyStreamSign 验证签名并返回 AccountID 和 RealIdentity
func VerifyStreamSign(signStr string) (uint, string, error) {
	parts := strings.Split(signStr, ":")
	if len(parts) != 5 {
		return 0, "", fmt.Errorf("invalid sign format")
	}

	accB64, expStr, sigHex, realIDB64, saltB64 := parts[0], parts[1], parts[2], parts[3], parts[4]

	accBytes, err := base64.RawURLEncoding.DecodeString(accB64)
	if err != nil {
		return 0, "", fmt.Errorf("invalid account encoding")
	}
	accID, err := strconv.ParseUint(string(accBytes), 10, 32)
	if err != nil {
		return 0, "", fmt.Errorf("invalid account id")
	}

	expiry, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("invalid expiry")
	}
	if time.Now().Unix() > expiry {
		return 0, "", fmt.Errorf("link expired")
	}

	realBytes, err := base64.RawURLEncoding.DecodeString(realIDB64)
	if err != nil {
		return 0, "", fmt.Errorf("invalid real identity encoding")
	}
	realIdentity := string(realBytes)

	// 重组 Payload 进行验证
	payload := fmt.Sprintf("%d:%d:%s:%s", accID, expiry, realIdentity, saltB64)
	mac := hmac.New(sha256.New, jwtSecret)
	mac.Write([]byte(payload))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expectedSig), []byte(sigHex)) {
		return 0, "", fmt.Errorf("signature mismatch")
	}

	return uint(accID), realIdentity, nil
}