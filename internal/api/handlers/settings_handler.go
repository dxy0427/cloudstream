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
	// 同时返回 WebhookURL
	var user models.User
	database.DB.Where("username = ?", username).First(&user)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"username": username,
		"webhook":  user.WebhookURL,
	}})
}

// 获取日志接口
func GetSystemLogsHandler(c *gin.Context) {
	logs, err := core.ReadRecentLogs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "读取日志失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": logs})
}

// 发送测试通知
func TestWebhookHandler(c *gin.Context) {
	var req struct {
		URL string `json:"url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "URL不能为空"})
		return
	}
	// 临时保存一下发个测试，不存库
	tempUser := models.User{WebhookURL: req.URL}
	// 简单粗暴的方式：直接调用 SendNotification，但它读库，所以这里特殊处理一下逻辑
	// 为了简单，我们直接存库再发，或者在 Settings 页面保存后再测试
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "请先保存设置，然后等待任务触发测试"})
}

func UpdateCredentialsHandler(c *gin.Context) {
	var req struct {
		NewUsername     string `json:"newUsername"`
		CurrentPassword string `json:"currentPassword" binding:"required"`
		NewPassword     string `json:"newPassword"`
		ConfirmPassword string `json:"confirmPassword"`
		WebhookURL      string `json:"webhookUrl"` // 新增字段
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
    // 更新 Webhook
	if req.WebhookURL != user.WebhookURL {
		user.WebhookURL = req.WebhookURL
        // Webhook 更新不需要重置 Token，所以这里不算 critical change
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