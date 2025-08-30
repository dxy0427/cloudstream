package auth

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/utils"
	"crypto/rand"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog/log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

var jwtSecret []byte

const secretFileName = ".jwt_secret"  // JWT密钥文件名
const secretFilePath = "./data/"      // 密钥存储目录

// init：初始化JWT密钥（优先加载文件，不存在则生成32字节强密钥并保存）
func init() {
	fullPath := filepath.Join(secretFilePath, secretFileName)

	// 从文件加载密钥（需32字节以上才有效）
	secret, err := os.ReadFile(fullPath)
	if err == nil && len(secret) >= 32 {
		jwtSecret = secret
		log.Info().Msg("已从文件加载 JWT 密钥")
		return
	}

	log.Warn().Msg("JWT 密钥文件不存在或无效，正在生成新的密钥...")

	// 生成32字节强随机密钥（256位，符合HS256要求）
	newSecret := make([]byte, 32)
	if _, err := rand.Read(newSecret); err != nil {
		log.Fatal().Err(err).Msg("无法生成新的 JWT 密钥")
	}

	// 确保密钥存储目录存在（权限0750：所有者读写执行，同组读执行）
	if err := os.MkdirAll(secretFilePath, 0750); err != nil {
		log.Fatal().Err(err).Msg("无法创建用于存储密钥的目录")
	}

	// 保存密钥到文件（权限0600：仅所有者可读写，防止泄露）
	if err := os.WriteFile(fullPath, newSecret, 0600); err != nil {
		log.Fatal().Err(err).Msg("无法保存新的 JWT 密钥到文件")
	}

	jwtSecret = newSecret
	log.Info().Str("path", fullPath).Msg("已成功生成并保存新的 JWT 密钥")
	log.Warn().Msg("此文件非常重要，请妥善保管，不要泄露")
}

// LoginRequest：登录请求参数结构体（用户名/密码必传）
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// LoginHandler：处理用户登录，校验凭证后生成JWT Token
func LoginHandler(c *gin.Context) {
	var req LoginRequest
	// 解析登录请求参数，格式错误返回400（提示用户名/密码不能为空）
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户名或密码不能为空"})
		return
	}

	// 按用户名查询用户，不存在返回401（统一提示“用户名或密码错误”，避免暴露用户存在性）
	var user models.User
	if err := database.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	// 校验密码哈希，不匹配返回401
	if !utils.CheckPasswordHash(req.Password, user.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	// 生成带版本号的JWT Token，失败返回500
	tokenString, err := generateToken(user.Username, user.TokenVersion)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法生成 Token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": tokenString})
}

// generateToken：生成JWT Token（含用户名、版本号，有效期7天，HS256签名）
func generateToken(username string, tokenVersion int) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"username": username,  // 存储当前登录用户名
		"version":  tokenVersion, // 存储Token版本（用于改密码/用户名后失效旧Token）
		"exp":      time.Now().Add(time.Hour * 24 * 7).Unix(), // 有效期7天
		"iat":      time.Now().Unix(), // 签发时间
	})
	return token.SignedString(jwtSecret)
}

// JWTAuthMiddleware：Gin JWT认证中间件（校验Token有效性、版本一致性）
func JWTAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 获取Authorization请求头，缺失返回401
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "请求未包含 Token"})
			return
		}

		// 2. 解析Bearer Token格式（如“Bearer xxx”），无效返回401
		var tokenString string
		fmt.Sscanf(authHeader, "Bearer %s", &tokenString)
		if tokenString == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token 格式不正确"})
			return
		}

		// 3. 验证Token签名（仅支持HS256），解析失败返回401
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

		// 4. 提取并校验Token中的用户名和版本号
		if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
			username, _ := claims["username"].(string)
			version, _ := claims["version"].(float64) // JWT claims默认是float64，需转换

			// 4.1 按用户名查用户，不存在返回401
			var user models.User
			if err := database.DB.Where("username = ?", username).First(&user).Error; err != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token对应的用户不存在"})
				return
			}

			// 4.2 Token版本与用户当前版本不一致则失效（改密码/用户名后旧Token无效）
			if int(version) != user.TokenVersion {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token 已失效，请重新登录"})
				return
			}

			// 5. 将用户名存入上下文，供后续接口使用
			c.Set("username", username)
			c.Next()
		} else {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token 无效"})
		}
	}
}