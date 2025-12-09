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
	database.DB.Save(&user)

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "安全退出成功"})
}