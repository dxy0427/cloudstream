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
	
	notifyType := user.NotifyType
	if notifyType == "" {
		notifyType = models.NotifyTypeWebhook
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"username":       username,
		"notifyType":     notifyType,
		"webhookUrl":     user.WebhookURL,
		"telegramToken":  user.TelegramToken,
		"telegramChatId": user.TelegramChatID,
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

func TestWebhookHandler(c *gin.Context) {
	var req map[string]string
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "参数错误"})
		return
	}
	
	if err := core.SendTestNotification(req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "测试发送失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "测试消息发送成功！"})
}

// 新增：专门更新通知配置的接口
func UpdateNotificationHandler(c *gin.Context) {
	var req struct {
		NotifyType      string `json:"notifyType"`
		WebhookURL      string `json:"webhookUrl"`
		TelegramToken   string `json:"telegramToken"`
		TelegramChatID  string `json:"telegramChatId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "参数错误"})
		return
	}

	currentUsername, _ := c.Get("username")
	var user models.User
	if err := database.DB.Where("username = ?", currentUsername).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "用户未找到"})
		return
	}

	user.NotifyType = req.NotifyType
	user.WebhookURL = req.WebhookURL
	user.TelegramToken = req.TelegramToken
	user.TelegramChatID = req.TelegramChatID
	
	if err := database.DB.Save(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "保存失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "通知设置已保存"})
}

// 安全设置：只负责改密码和用户名
func UpdateCredentialsHandler(c *gin.Context) {
	var req struct {
		NewUsername     string `json:"newUsername"`
		CurrentPassword string `json:"currentPassword" binding:"required"`
		NewPassword     string `json:"newPassword"`
		ConfirmPassword string `json:"confirmPassword"`
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
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "未做任何修改"})
	}
}