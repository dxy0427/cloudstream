package handlers

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"github.com/gin-gonic/gin"
	"net/http"
)

// LogoutHandler 强制使当前用户的旧 Token 失效
func LogoutHandler(c *gin.Context) {
	username, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "未登录"})
		return
	}

	var user models.User
	if err := database.DB.Where("username = ?", username).First(&user).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "用户不存在"})
		return
	}

	// 核心逻辑：版本号自增
	user.TokenVersion++
	if err := database.DB.Save(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "退出登录失败，请重试"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "安全退出成功"})
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

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"siteTitle": user.SiteTitle,
			"theme":     user.Theme,
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

	// 验证主题值
	if req.Theme != "dark" && req.Theme != "light" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "主题值必须是 dark 或 light"})
		return
	}

	// 设置默认值
	if req.SiteTitle == "" {
		req.SiteTitle = "CloudStream"
	}
	if req.Theme == "" {
		req.Theme = "dark"
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