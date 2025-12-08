package handlers

import (
	"cloudstream/internal/core"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/utils"
	"github.com/gin-gonic/gin"
	"net/http"
)

func GetUsernameHandler(c *gin.Context) {
	username, _ := c.Get("username")
	var user models.User
	database.DB.Where("username = ?", username).First(&user)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"username": username,
		"webhook":  user.WebhookURL,
	}})
}

func GetSystemLogsHandler(c *gin.Context) {
	logs, err := core.ReadRecentLogs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "读取日志失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": logs})
}

// 新增：Webhook 测试接口
func TestWebhookHandler(c *gin.Context) {
	var req struct {
		URL string `json:"url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "参数错误"})
		return
	}
	
	if err := core.SendTestNotification(req.URL); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "发送失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "发送成功，请检查接收端"})
}

func UpdateCredentialsHandler(c *gin.Context) {
	var req struct {
		NewUsername     string `json:"newUsername"`
		CurrentPassword string `json:"currentPassword" binding:"required"`
		NewPassword     string `json:"newPassword"`
		ConfirmPassword string `json:"confirmPassword"`
		WebhookURL      string `json:"webhookUrl"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "参数错误"})
		return
	}

	currentUsername, _ := c.Get("username")
	var user models.User
	database.DB.Where("username = ?", currentUsername).First(&user)

	if !utils.CheckPasswordHash(req.CurrentPassword, user.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 1, "message": "当前密码不正确"})
		return
	}

	changed := false
	if req.WebhookURL != user.WebhookURL {
		user.WebhookURL = req.WebhookURL
		database.DB.Save(&user)
	}

	if req.NewUsername != "" && req.NewUsername != user.Username {
		var existingUser models.User
		if database.DB.Where("username = ?", req.NewUsername).First(&existingUser).Error == nil {
			c.JSON(http.StatusConflict, gin.H{"code": 1, "message": "新用户名已被占用"})
			return
		}
		user.Username = req.NewUsername
		changed = true
	}

	if req.NewPassword != "" {
		if req.NewPassword != req.ConfirmPassword {
			c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "两次输入的新密码不一致"})
			return
		}
		newPasswordHash, err := utils.HashPassword(req.NewPassword)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "密码加密失败"})
			return
		}
		user.PasswordHash = newPasswordHash
		changed = true
	}

	if changed {
		user.TokenVersion++
		if err := database.DB.Save(&user).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "更新凭证失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "凭证更新成功，请重新登录"})
	} else {
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "设置已更新"})
	}
}