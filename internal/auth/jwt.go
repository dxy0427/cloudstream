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
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog/log"
)

var jwtSecret []byte

const (
	secretFileName      = ".jwt_secret"
	secretDirPath       = "./data/"
	contextTokenVersion = "token_version"
	contextTokenExpiry  = "exp"
)

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
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("cloudstream_token", tokenString, 7*24*3600, "/", "", IsSecureRequest(c), true)
	c.JSON(http.StatusOK, gin.H{
		"code":                  0,
		"needsPasswordReminder": user.NeedsPasswordReminder,
		"passwordReminderShown": user.PasswordReminderShown,
	})
}

// IsSecureRequest 判断当前请求是否应使用 Secure Cookie（含反向代理 HTTPS 终止场景）。
func IsSecureRequest(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	if !isTrustedLocalProxy(c) {
		return false
	}
	proto := c.GetHeader("X-Forwarded-Proto")
	if proto == "" {
		proto = c.GetHeader("X-Forwarded-Protocol")
	}
	if i := strings.IndexByte(proto, ','); i >= 0 {
		proto = proto[:i]
	}
	return strings.EqualFold(strings.TrimSpace(proto), "https")
}

func isTrustedLocalProxy(c *gin.Context) bool {
	if c.Request.RemoteAddr == "" {
		return true
	}
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		host = c.Request.RemoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
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
		var tokenString string
		if authHeader := c.GetHeader("Authorization"); authHeader != "" {
			if strings.HasPrefix(authHeader, "Bearer ") {
				tokenString = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
			}
		}
		if tokenString == "" {
			if cookieToken, err := c.Cookie("cloudstream_token"); err == nil {
				tokenString = cookieToken
			}
		}
		// 不再从 query 读取 token，避免写入访问日志 / Referer 泄露
		if tokenString == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "请求未包含 Token"})
			return
		}
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			return jwtSecret, nil
		}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token 无效或已过期"})
			return
		}
		if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
			username, usernameOK := claims["username"].(string)
			version, versionOK := numericClaimAsInt(claims["version"])
			expiresAt, expErr := claims.GetExpirationTime()
			if !usernameOK || username == "" || !versionOK || expErr != nil || expiresAt == nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token 无效"})
				return
			}
			c.Set("username", username)
			c.Set(contextTokenVersion, version)
			c.Set(contextTokenExpiry, expiresAt.Time.Unix())
			if !SessionStillValid(c) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token 已失效，请重新登录"})
				return
			}
			c.Next()
		} else {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token 无效"})
		}
	}
}

// SessionStillValid rechecks the database-backed session state for long-lived handlers.
func SessionStillValid(c *gin.Context) bool {
	usernameValue, usernameOK := c.Get("username")
	versionValue, versionOK := c.Get(contextTokenVersion)
	expiryValue, expiryOK := c.Get(contextTokenExpiry)
	username, usernameTypeOK := usernameValue.(string)
	version, versionTypeOK := versionValue.(int)
	expiresAt, expiryTypeOK := expiryValue.(int64)
	if !usernameOK || !versionOK || !expiryOK || !usernameTypeOK || !versionTypeOK || !expiryTypeOK || username == "" {
		return false
	}
	if time.Now().Unix() >= expiresAt {
		return false
	}

	var count int64
	if err := database.DB.Model(&models.User{}).
		Where("username = ? AND token_version = ?", username, version).
		Count(&count).Error; err != nil {
		return false
	}
	return count == 1
}

func numericClaimAsInt(value interface{}) (int, bool) {
	number, ok := value.(float64)
	if !ok || number < 0 || number != float64(int(number)) {
		return 0, false
	}
	return int(number), true
}

func SignAccountStreamURL(accountID uint, realIdentity string, expireHours int) (string, error) {
	if len(jwtSecret) == 0 {
		return "", fmt.Errorf("secret not initialized")
	}
	if accountID == 0 {
		return "", fmt.Errorf("account id is required")
	}
	if expireHours < 0 {
		return "", fmt.Errorf("expire hours cannot be negative")
	}

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

	payload := fmt.Sprintf("%d:%d:%s:%s", accountID, expiry, realIdentity, saltB64)
	mac := hmac.New(sha256.New, jwtSecret)
	mac.Write([]byte(payload))
	sigHex := hex.EncodeToString(mac.Sum(nil))

	return fmt.Sprintf("%s:%s:%s:%s:%s", accB64, expStr, sigHex, realIDB64, saltB64), nil
}

func VerifyAccountStreamSign(signStr string) (uint, string, error) {
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
	if expiry != 0 && time.Now().Unix() > expiry {
		return 0, "", fmt.Errorf("link expired")
	}

	realBytes, err := base64.RawURLEncoding.DecodeString(realIDB64)
	if err != nil {
		return 0, "", fmt.Errorf("invalid real identity encoding")
	}
	realIdentity := string(realBytes)

	payload := fmt.Sprintf("%d:%d:%s:%s", accID, expiry, realIdentity, saltB64)
	mac := hmac.New(sha256.New, jwtSecret)
	mac.Write([]byte(payload))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expectedSig), []byte(sigHex)) {
		return 0, "", fmt.Errorf("signature mismatch")
	}

	return uint(accID), realIdentity, nil
}
