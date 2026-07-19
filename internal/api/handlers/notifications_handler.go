package handlers

import (
	"cloudstream/internal/core"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const maxNotificationsPerUser = 32

var notificationMutationMu sync.Mutex

type notificationMutationRequest struct {
	ID               uint    `json:"ID"`
	Name             *string `json:"Name"`
	Type             *string `json:"Type"`
	WebhookURL       *string `json:"WebhookURL"`
	TelegramToken    *string `json:"TelegramToken"`
	TelegramChatID   *string `json:"TelegramChatID"`
	Enabled          *bool   `json:"Enabled"`
	NotifyOnComplete *bool   `json:"NotifyOnComplete"`
	NotifyOnError    *bool   `json:"NotifyOnError"`
	NotifyOnStop     *bool   `json:"NotifyOnStop"`
	NotifyOnManual   *bool   `json:"NotifyOnManual"`
	Version          *int    `json:"Version"`
}

func currentUser(c *gin.Context) (models.User, bool) {
	username, _ := c.Get("username")
	var user models.User
	if err := database.DB.Where("username = ?", username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "用户未找到"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "读取用户信息失败"})
		}
		return models.User{}, false
	}
	return user, true
}

func sanitizeNotification(notification models.Notification) gin.H {
	return gin.H{
		"ID": notification.ID, "CreatedAt": notification.CreatedAt, "UpdatedAt": notification.UpdatedAt,
		"Name": notification.Name, "Type": notification.Type, "Version": notification.Version,
		"Enabled": notification.Enabled, "NotifyOnComplete": notification.NotifyOnComplete,
		"NotifyOnError": notification.NotifyOnError, "NotifyOnStop": notification.NotifyOnStop,
		"NotifyOnManual": notification.NotifyOnManual,
	}
}

func notificationDetail(notification models.Notification) gin.H {
	return gin.H{
		"ID": notification.ID, "CreatedAt": notification.CreatedAt, "UpdatedAt": notification.UpdatedAt,
		"Name": notification.Name, "Type": notification.Type, "Version": notification.Version,
		"WebhookURL": notification.WebhookURL, "TelegramToken": notification.TelegramToken, "TelegramChatID": notification.TelegramChatID,
		"Enabled": notification.Enabled, "NotifyOnComplete": notification.NotifyOnComplete,
		"NotifyOnError": notification.NotifyOnError, "NotifyOnStop": notification.NotifyOnStop,
		"NotifyOnManual": notification.NotifyOnManual,
	}
}

func ListNotificationsHandler(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		return
	}
	var notifications []models.Notification
	if err := database.DB.Where("user_id = ?", user.ID).Order("id asc").Find(&notifications).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "读取通知列表失败"})
		return
	}
	data := make([]gin.H, 0, len(notifications))
	for _, notification := range notifications {
		data = append(data, sanitizeNotification(notification))
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": data})
}

func GetNotificationHandler(c *gin.Context) {
	setSensitiveResponseHeaders(c)
	notificationID, ok := parseNotificationID(c)
	if !ok {
		return
	}
	user, ok := currentUser(c)
	if !ok {
		return
	}

	var notification models.Notification
	if err := database.DB.Where("id = ? AND user_id = ?", notificationID, user.ID).First(&notification).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "通知目标未找到"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "读取通知详情失败"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": notificationDetail(notification)})
}

func CreateNotificationHandler(c *gin.Context) {
	var req notificationMutationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "通知参数错误"})
		return
	}
	user, ok := currentUser(c)
	if !ok {
		return
	}

	notificationMutationMu.Lock()
	defer notificationMutationMu.Unlock()
	var notification models.Notification
	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&models.Notification{}).Where("user_id = ?", user.ID).Count(&count).Error; err != nil {
			return err
		}
		if count >= maxNotificationsPerUser {
			return newAPIMutationError(http.StatusConflict, "通知目标数量已达上限")
		}
		notification = models.Notification{UserID: user.ID}
		notification.Version = 1
		applyNotificationDefaults(&notification)
		if err := applyNotificationRequest(&notification, req); err != nil {
			return newAPIMutationError(http.StatusBadRequest, err.Error())
		}
		requested := notification
		if err := tx.Create(&notification).Error; err != nil {
			return err
		}
		if err := tx.Model(&notification).Updates(notificationBooleanUpdates(requested)).Error; err != nil {
			return err
		}
		return tx.First(&notification, notification.ID).Error
	}); err != nil {
		respondMutationError(c, err, "创建通知目标失败", "通知名称已存在")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": sanitizeNotification(notification)})
}

func UpdateNotificationHandler(c *gin.Context) {
	notificationID, ok := parseNotificationID(c)
	if !ok {
		return
	}
	var req notificationMutationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "通知参数错误"})
		return
	}
	user, ok := currentUser(c)
	if !ok {
		return
	}

	notificationMutationMu.Lock()
	defer notificationMutationMu.Unlock()
	var notification models.Notification
	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		var stored models.Notification
		if err := tx.Where("id = ? AND user_id = ?", notificationID, user.ID).First(&stored).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newAPIMutationError(http.StatusNotFound, "通知目标未找到")
			}
			return err
		}
		notification = stored
		if req.Version == nil || *req.Version != stored.Version {
			return newAPIMutationError(http.StatusConflict, "通知配置已发生变化，请刷新后重试")
		}
		if err := applyNotificationRequest(&notification, req); err != nil {
			return newAPIMutationError(http.StatusBadRequest, err.Error())
		}
		notification.Version = stored.Version + 1
		result := tx.Model(&models.Notification{}).Where("id = ? AND user_id = ? AND version = ?", notificationID, user.ID, stored.Version).Updates(notificationUpdates(notification))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return newAPIMutationError(http.StatusConflict, "通知配置已发生变化，请刷新后重试")
		}
		return tx.First(&notification, notificationID).Error
	}); err != nil {
		respondMutationError(c, err, "更新通知目标失败", "通知名称已存在")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": sanitizeNotification(notification)})
}

func DeleteNotificationHandler(c *gin.Context) {
	notificationID, ok := parseNotificationID(c)
	if !ok {
		return
	}
	versionValue, err := strconv.Atoi(c.Query("version"))
	if err != nil || versionValue < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "无效的通知版本"})
		return
	}
	user, ok := currentUser(c)
	if !ok {
		return
	}
	notificationMutationMu.Lock()
	defer notificationMutationMu.Unlock()
	result := database.DB.Unscoped().Where("id = ? AND user_id = ? AND version = ?", notificationID, user.ID, versionValue).Delete(&models.Notification{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "删除通知目标失败"})
		return
	}
	if result.RowsAffected != 1 {
		var count int64
		if err := database.DB.Model(&models.Notification{}).Where("id = ? AND user_id = ?", notificationID, user.ID).Count(&count).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "检查通知目标失败"})
			return
		}
		if count == 1 {
			c.JSON(http.StatusConflict, gin.H{"code": 1, "message": "通知配置已发生变化，请刷新后重试"})
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "通知目标未找到"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "通知目标已删除"})
}

func TestNotificationHandler(c *gin.Context) {
	var req notificationMutationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "通知参数错误"})
		return
	}
	user, ok := currentUser(c)
	if !ok {
		return
	}

	var notification models.Notification
	if req.ID != 0 {
		var existing models.Notification
		if err := database.DB.Where("id = ? AND user_id = ?", req.ID, user.ID).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "通知目标未找到"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "读取通知目标失败"})
			}
			return
		}
		notification = existing
		if req.Version == nil || *req.Version != existing.Version {
			c.JSON(http.StatusConflict, gin.H{"code": 1, "message": "通知配置已发生变化，请刷新后重试"})
			return
		}
	} else {
		notification.UserID = user.ID
		applyNotificationDefaults(&notification)
	}
	if err := applyNotificationRequest(&notification, req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}
	if err := core.SendTestNotification(notification); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 1, "message": "测试通知发送失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "测试通知发送成功"})
}

func applyNotificationDefaults(notification *models.Notification) {
	notification.Enabled = true
	notification.NotifyOnComplete = true
	notification.NotifyOnError = true
	notification.NotifyOnStop = true
	notification.NotifyOnManual = true
}

func applyNotificationRequest(notification *models.Notification, req notificationMutationRequest) error {
	if req.Name != nil {
		notification.Name = strings.TrimSpace(*req.Name)
	}
	if req.Type != nil {
		notification.Type = strings.TrimSpace(*req.Type)
	}
	if req.Enabled != nil {
		notification.Enabled = *req.Enabled
	}
	if req.NotifyOnComplete != nil {
		notification.NotifyOnComplete = *req.NotifyOnComplete
	}
	if req.NotifyOnError != nil {
		notification.NotifyOnError = *req.NotifyOnError
	}
	if req.NotifyOnStop != nil {
		notification.NotifyOnStop = *req.NotifyOnStop
	}
	if req.NotifyOnManual != nil {
		notification.NotifyOnManual = *req.NotifyOnManual
	}
	if req.WebhookURL != nil {
		notification.WebhookURL = strings.TrimSpace(*req.WebhookURL)
	}
	if req.TelegramToken != nil {
		notification.TelegramToken = strings.TrimSpace(*req.TelegramToken)
	}
	if req.TelegramChatID != nil {
		notification.TelegramChatID = strings.TrimSpace(*req.TelegramChatID)
	}

	if notification.Name == "" {
		return errors.New("通知名称不能为空")
	}
	if len([]rune(notification.Name)) > 64 {
		return errors.New("通知名称不能超过 64 个字符")
	}
	switch notification.Type {
	case models.NotifyTypeWebhook:
		if notification.WebhookURL == "" {
			return errors.New("Webhook URL 不能为空")
		}
		if err := validateWebhookURL(notification.WebhookURL); err != nil {
			return err
		}
		notification.TelegramToken = ""
		notification.TelegramChatID = ""
	case models.NotifyTypeTelegram:
		if notification.TelegramToken == "" || notification.TelegramChatID == "" {
			return errors.New("Telegram Token 和 Chat ID 不能为空")
		}
		if len(notification.TelegramToken) > 512 || len(notification.TelegramChatID) > 256 {
			return errors.New("Telegram 配置过长")
		}
		notification.WebhookURL = ""
	default:
		return errors.New("不支持的通知类型")
	}
	return nil
}

func validateWebhookURL(value string) error {
	if len(value) > 4096 {
		return errors.New("Webhook URL 过长")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("Webhook URL 必须是有效的 HTTP 或 HTTPS 地址")
	}
	return nil
}

func notificationBooleanUpdates(notification models.Notification) map[string]interface{} {
	return map[string]interface{}{
		"enabled": notification.Enabled, "notify_on_complete": notification.NotifyOnComplete,
		"notify_on_error": notification.NotifyOnError, "notify_on_stop": notification.NotifyOnStop,
		"notify_on_manual": notification.NotifyOnManual,
	}
}

func notificationUpdates(notification models.Notification) map[string]interface{} {
	updates := notificationBooleanUpdates(notification)
	updates["name"] = notification.Name
	updates["type"] = notification.Type
	updates["webhook_url"] = notification.WebhookURL
	updates["telegram_token"] = notification.TelegramToken
	updates["telegram_chat_id"] = notification.TelegramChatID
	updates["version"] = notification.Version
	return updates
}

func parseNotificationID(c *gin.Context) (uint, bool) {
	value, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || value == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "无效的通知ID"})
		return 0, false
	}
	return uint(value), true
}
