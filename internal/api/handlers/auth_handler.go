package handlers

import (
	"cloudstream/internal/auth"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// LogoutHandler 退出登录，使当前 Token 失效
func LogoutHandler(c *gin.Context) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("cloudstream_token", "", -1, "/", "", auth.IsSecureRequest(c), true)

	username, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "退出成功"})
		return
	}
	tokenVersion, versionExists := c.Get("token_version")
	if !versionExists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 1, "message": "会话无效"})
		return
	}

	// 递增 TokenVersion 使所有已签发的 Token 失效
	result := database.DB.Model(&models.User{}).
		Where("username = ? AND token_version = ?", username, tokenVersion).
		UpdateColumn("token_version", gorm.Expr("token_version + 1"))
	if result.Error != nil || result.RowsAffected != 1 {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "退出失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "退出成功"})
}

// GetUserSettingsHandler 获取用户个性化设置
func GetUserSettingsHandler(c *gin.Context) {
	username, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 1, "message": "未登录"})
		return
	}

	var user models.User
	if err := database.DB.Where("username = ?", username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "用户不存在"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "读取用户设置失败"})
		}
		return
	}

	theme := user.Theme
	if theme == "" {
		theme = "light"
	}
	siteTitle := user.SiteTitle
	if siteTitle == "" {
		siteTitle = "CloudStream"
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"siteTitle": siteTitle,
			"theme":     theme,
		},
	})
}

// UpdateUserSettingsHandler 更新用户个性化设置
func UpdateUserSettingsHandler(c *gin.Context) {
	username, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 1, "message": "未登录"})
		return
	}

	var req struct {
		SiteTitle string `json:"siteTitle"`
		Theme     string `json:"theme"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "参数错误"})
		return
	}

	req.SiteTitle = strings.TrimSpace(req.SiteTitle)
	if req.SiteTitle == "" {
		req.SiteTitle = "CloudStream"
	}
	if len([]rune(req.SiteTitle)) > 64 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "网站标题不能超过 64 个字符"})
		return
	}
	if req.Theme == "" {
		req.Theme = "light"
	}
	if req.Theme != "dark" && req.Theme != "light" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "主题值必须是 dark 或 light"})
		return
	}

	result := database.DB.Model(&models.User{}).
		Where("username = ?", username).
		Updates(map[string]interface{}{
			"site_title": req.SiteTitle,
			"theme":      req.Theme,
		})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "更新设置失败"})
		return
	}
	if result.RowsAffected != 1 {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "用户不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "设置更新成功",
		"data": gin.H{
			"siteTitle": req.SiteTitle,
			"theme":     req.Theme,
		},
	})
}
