package handlers

import (
	"cloudstream/internal/core"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/utils"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
)

func GetUsernameHandler(c *gin.Context) {
	username, _ := c.Get("username")
	var user models.User
	if err := database.DB.Where("username = ?", username).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "用户不存在"})
		return
	}

	notifyType := user.NotifyType
	if notifyType == "" {
		notifyType = models.NotifyTypeWebhook
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"username":              username,
		"notifyType":            notifyType,
		"webhookUrl":            user.WebhookURL,
		"telegramToken":         user.TelegramToken,
		"telegramChatId":        user.TelegramChatID,
		"notifyOnComplete":      user.NotifyOnComplete,
		"notifyOnError":         user.NotifyOnError,
		"notifyOnStop":          user.NotifyOnStop,
		"notifyOnManual":        user.NotifyOnManual,
		"needsPasswordReminder": user.NeedsPasswordReminder,
		"passwordReminderShown": user.PasswordReminderShown,
	}})
}

func GetDashboardStatsHandler(c *gin.Context) {
	var accountCount int64
	var taskCount int64
	var runningTaskCount int64

	if err := database.DB.Model(&models.Account{}).Count(&accountCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "读取账户统计失败"})
		return
	}
	if err := database.DB.Model(&models.Task{}).Count(&taskCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "读取任务统计失败"})
		return
	}
	var taskIDs []uint
	if err := database.DB.Model(&models.Task{}).Pluck("id", &taskIDs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "读取运行中任务统计失败"})
		return
	}
	for _, id := range taskIDs {
		if core.IsTaskRunning(id) {
			runningTaskCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"accounts":     accountCount,
		"tasks":        taskCount,
		"runningTasks": runningTaskCount,
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

func StreamSystemLogsHandler(c *gin.Context) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "stream not supported"})
		return
	}

	logs, offset, err := core.ReadRecentLogsWithOffset()
	if err == nil && len(logs) > 0 {
		for _, line := range logs {
			fmt.Fprintf(c.Writer, "data: %s\n\n", line)
		}
		flusher.Flush()
	}

	lastHeartbeat := time.Now()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-time.After(2 * time.Second):
			lines, newOffset, err := core.ReadLogFromOffset(offset)
			if err == nil {
				offset = newOffset
				for _, line := range lines {
					fmt.Fprintf(c.Writer, "data: %s\n\n", line)
				}
				if len(lines) > 0 {
					lastHeartbeat = time.Now()
					flusher.Flush()
					continue
				}
			}
			if time.Since(lastHeartbeat) >= 15*time.Second {
				fmt.Fprintf(c.Writer, ": ping\n\n")
				lastHeartbeat = time.Now()
				flusher.Flush()
			}
		}
	}
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

func UpdateNotificationHandler(c *gin.Context) {
	var req struct {
		NotifyType       string `json:"notifyType"`
		WebhookURL       string `json:"webhookUrl"`
		TelegramToken    string `json:"telegramToken"`
		TelegramChatID   string `json:"telegramChatId"`
		NotifyOnComplete *bool  `json:"notifyOnComplete"`
		NotifyOnError    *bool  `json:"notifyOnError"`
		NotifyOnStop     *bool  `json:"notifyOnStop"`
		NotifyOnManual   *bool  `json:"notifyOnManual"`
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
	if req.NotifyOnComplete != nil {
		user.NotifyOnComplete = *req.NotifyOnComplete
	}
	if req.NotifyOnError != nil {
		user.NotifyOnError = *req.NotifyOnError
	}
	if req.NotifyOnStop != nil {
		user.NotifyOnStop = *req.NotifyOnStop
	}
	if req.NotifyOnManual != nil {
		user.NotifyOnManual = *req.NotifyOnManual
	}

	if err := database.DB.Save(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "保存失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "通知设置已保存"})
}

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
	if err := database.DB.Where("username = ?", currentUsername).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "用户不存在"})
		return
	}

	matched, needsUpgrade, upgradeHash := utils.CheckAndUpgradePassword(req.CurrentPassword, user.PasswordHash)
	if !matched {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 1, "message": "当前密码不正确"})
		return
	}
	if needsUpgrade && upgradeHash != "" {
		database.DB.Model(&user).Updates(map[string]interface{}{
			"password_hash":    upgradeHash,
			"password_version": 1,
		})
		user.PasswordHash = upgradeHash
	}

	changed := false
	passwordChanged := false
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
		user.PasswordVersion = 1
		user.NeedsPasswordReminder = false
		user.PasswordReminderShown = true
		passwordChanged = true
		changed = true
	}

	if changed {
		user.TokenVersion++
		if err := database.DB.Save(&user).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "更新凭证失败: " + err.Error()})
			return
		}
		if passwordChanged {
			c.JSON(http.StatusOK, gin.H{"code": 0, "message": "凭证更新成功，请重新登录", "data": gin.H{"needsPasswordReminder": false, "passwordReminderShown": true}})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "凭证更新成功，请重新登录"})
	} else {
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "未做任何修改"})
	}
}

func DismissPasswordReminderHandler(c *gin.Context) {
	currentUsername, _ := c.Get("username")
	var user models.User
	if err := database.DB.Where("username = ?", currentUsername).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "用户未找到"})
		return
	}
	user.PasswordReminderShown = true
	if err := database.DB.Model(&user).Update("password_reminder_shown", true).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "更新提醒状态失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已忽略提醒"})
}
