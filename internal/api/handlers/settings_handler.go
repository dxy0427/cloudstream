package handlers

import (
	"cloudstream/internal/core"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/utils"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GetUsernameHandler(c *gin.Context) {
	username, _ := c.Get("username")
	var user models.User
	if err := database.DB.Where("username = ?", username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "用户不存在"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "读取用户设置失败"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"username":              username,
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

	logs, cursor, err := core.ReadRecentLogsWithCursor()
	if err == nil && len(logs) > 0 {
		for _, line := range logs {
			c.SSEvent("", line)
		}
		flusher.Flush()
	}

	pollTicker := time.NewTicker(2 * time.Second)
	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer pollTicker.Stop()
	defer heartbeatTicker.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-pollTicker.C:
			lines, newCursor, err := core.ReadLogsFromCursor(cursor)
			if err == nil {
				cursor = newCursor
				for _, line := range lines {
					c.SSEvent("", line)
				}
				if len(lines) > 0 {
					flusher.Flush()
				}
			}
		case <-heartbeatTicker.C:
			_, _ = c.Writer.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
	}
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
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "用户不存在"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "读取用户凭证失败"})
		}
		return
	}

	matched, needsUpgrade, upgradeHash := utils.CheckAndUpgradePassword(req.CurrentPassword, user.PasswordHash)
	if !matched {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "当前密码不正确"})
		return
	}
	updates := make(map[string]interface{})
	passwordChanged := false
	req.NewUsername = strings.TrimSpace(req.NewUsername)
	if req.NewUsername != "" && req.NewUsername != user.Username {
		if len([]rune(req.NewUsername)) > 64 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "用户名不能超过 64 个字符"})
			return
		}
		updates["username"] = req.NewUsername
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
		updates["password_hash"] = newPasswordHash
		updates["password_version"] = 1
		updates["needs_password_reminder"] = false
		updates["password_reminder_shown"] = true
		passwordChanged = true
	}
	if needsUpgrade && upgradeHash != "" && !passwordChanged {
		updates["password_hash"] = upgradeHash
		updates["password_version"] = 1
	}

	if len(updates) == 0 {
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "未做任何修改"})
		return
	}

	updates["token_version"] = gorm.Expr("token_version + 1")
	result := database.DB.Model(&models.User{}).
		Where("id = ? AND username = ? AND password_hash = ? AND token_version = ?", user.ID, user.Username, user.PasswordHash, user.TokenVersion).
		Updates(updates)
	if result.Error != nil {
		if isUniqueConstraintError(result.Error) {
			c.JSON(http.StatusConflict, gin.H{"code": 1, "message": "新用户名已被占用"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "更新凭证失败"})
		}
		return
	}
	if result.RowsAffected != 1 {
		c.JSON(http.StatusConflict, gin.H{"code": 1, "message": "凭证已发生变化，请重新登录"})
		return
	}
	if passwordChanged {
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "凭证更新成功，请重新登录", "data": gin.H{"needsPasswordReminder": false, "passwordReminderShown": true}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "凭证更新成功，请重新登录"})
}

func DismissPasswordReminderHandler(c *gin.Context) {
	currentUsername, _ := c.Get("username")
	result := database.DB.Model(&models.User{}).
		Where("username = ?", currentUsername).
		UpdateColumn("password_reminder_shown", true)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "更新提醒状态失败"})
		return
	}
	if result.RowsAffected != 1 {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "用户未找到"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已忽略提醒"})
}

func isUniqueConstraintError(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint") || strings.Contains(message, "duplicate")
}
