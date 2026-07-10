package handlers

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
)

// LogoutHandler 退出登录，使当前 Token 失效
func LogoutHandler(c *gin.Context) {
	c.SetSameSite(http.SameSiteStrictMode)
	secure := c.Request.TLS != nil
	c.SetCookie("cloudstream_token", "", -1, "/", "", secure, true)

	username, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "退出成功"})
		return
	}

	// 递增 TokenVersion 使所有已签发的 Token 失效
	if err := database.DB.Model(&models.User{}).Where("username = ?", username).Update("token_version", gorm.Expr("token_version + 1")).Error; err != nil {
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
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "用户不存在"})
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

	if req.SiteTitle == "" {
		req.SiteTitle = "CloudStream"
	}
	if req.Theme == "" {
		req.Theme = "light"
	}
	if req.Theme != "dark" && req.Theme != "light" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "主题值必须是 dark 或 light"})
		return
	}

	var user models.User
	if err := database.DB.Where("username = ?", username).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "用户不存在"})
		return
	}

	user.SiteTitle = req.SiteTitle
	user.Theme = req.Theme

	if err := database.DB.Save(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "更新设置失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "设置更新成功",
		"data": gin.H{
			"siteTitle": user.SiteTitle,
			"theme":     user.Theme,
		},
	})
}
