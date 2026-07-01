package handlers

import (
	"cloudstream/internal/auth"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/openlist"
	"cloudstream/internal/pan123"
	"cloudstream/internal/webdav"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"net/http"
	"strconv"
	"strings"
)

// verifyStreamJWT 校验无签名模式下的 JWT Token
func verifyStreamJWT(c *gin.Context) bool {
	var tokenString string

	authHeader := c.GetHeader("Authorization")
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
		return false
	}

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return auth.GetJWTSecret(), nil
	})
	if err != nil || !token.Valid {
		return false
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return false
	}

	username, _ := claims["username"].(string)
	version, _ := claims["version"].(float64)
	if username == "" {
		return false
	}

	var user models.User
	if err := database.DB.Where("username = ?", username).First(&user).Error; err != nil {
		return false
	}
	if int(version) != user.TokenVersion {
		return false
	}

	c.Set("username", username)
	return true
}

func UnifiedStreamHandler(c *gin.Context) {
	rawPath := c.Param("path")
	sign := c.Query("sign")

	var accountID uint
	var identifier interface{}
	var account models.Account

	if sign != "" {
		taskID, accID, realIdentity, err := auth.VerifyStreamSign(sign)
		if err != nil {
			c.String(http.StatusForbidden, "Invalid signature: "+err.Error())
			return
		}
		accountID = accID

		var task models.Task
		if err := database.DB.First(&task, taskID).Error; err != nil {
			c.String(http.StatusNotFound, "Task not found")
			return
		}
		if task.AccountID != accountID {
			c.String(http.StatusForbidden, "Task/account mismatch")
			return
		}
		if !task.Enabled || !task.EncodePath {
			c.String(http.StatusForbidden, "Signed access is disabled for this task")
			return
		}

		if err := database.DB.First(&account, accountID).Error; err != nil {
			c.String(http.StatusNotFound, "Account not found")
			return
		}

		if strings.HasPrefix(realIdentity, "/") {
			identifier = realIdentity
		} else {
			if id, err := strconv.ParseInt(realIdentity, 10, 64); err == nil {
				identifier = id
			} else {
				identifier = realIdentity
			}
		}
	} else {
		// 无签名：必须通过 JWT 认证
		if !verifyStreamJWT(c) {
			c.String(http.StatusUnauthorized, "Authentication required: provide a valid sign or JWT token")
			return
		}

		trimmedPath := strings.TrimPrefix(rawPath, "/")
		parts := strings.Split(trimmedPath, "/")

		if len(parts) < 2 {
			c.String(http.StatusBadRequest, "Invalid URL format")
			return
		}

		idUint, err := strconv.ParseUint(parts[0], 10, 32)
		if err != nil {
			c.String(http.StatusBadRequest, "Invalid Account ID")
			return
		}
		accountID = uint(idUint)

		if err := database.DB.First(&account, accountID).Error; err != nil {
			c.String(http.StatusNotFound, "Account not found")
			return
		}

		if account.Type == models.AccountTypeOpenList || account.Type == models.AccountTypeWebDAV {
			pathPart := "/" + strings.Join(parts[1:], "/")
			identifier = strings.ReplaceAll(pathPart, "//", "/")
		} else {
			fileIdStr := parts[1]
			fileId, err := strconv.ParseInt(fileIdStr, 10, 64)
			if err != nil {
				c.String(http.StatusBadRequest, "Invalid File ID format for 123Pan")
				return
			}
			identifier = fileId
		}
	}

	// 根据账户类型选择客户端获取下载链接
	var downloadURL string
	var err error

	switch account.Type {
	case models.AccountTypeOpenList:
		client := openlist.NewClient(account)
		pathStr, ok := identifier.(string)
		if !ok {
			c.String(http.StatusBadRequest, "OpenList requires path identifier")
			return
		}
		downloadURL, err = client.GetRawURL(pathStr)
	case models.AccountTypeWebDAV:
		client := webdav.NewClient(account)
		pathStr, ok := identifier.(string)
		if !ok {
			c.String(http.StatusBadRequest, "WebDAV requires path identifier")
			return
		}
		downloadURL, err = client.GetDownloadURL(pathStr)
	default:
		client := pan123.NewClient(account)
		downloadURL, err = client.GetDownloadURL(identifier)
	}

	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("Failed to get link: %v", err))
		return
	}
	c.Redirect(http.StatusFound, downloadURL)
}
