package core

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/rs/zerolog/log"
)

type NotifyEvent string

const (
	NotifyEventComplete NotifyEvent = "complete"
	NotifyEventError    NotifyEvent = "error"
	NotifyEventStop     NotifyEvent = "stop"
	NotifyEventManual   NotifyEvent = "manual"
)

const notificationWorkerCount = 8

type notificationJob struct {
	notification models.Notification
	title        string
	message      string
	result       chan error
}

var notificationJobs = make(chan notificationJob, 256)

func init() {
	for range notificationWorkerCount {
		go func() {
			for job := range notificationJobs {
				job.result <- sendNotification(job.notification, job.title, job.message)
				close(job.result)
			}
		}()
	}
}

func SendNotificationByEvent(title, message string, event NotifyEvent) {
	sendNotificationWithEvent(title, message, event)
}

func sendNotificationWithEvent(title, message string, event NotifyEvent) {
	var notifications []models.Notification
	if err := database.DB.Where("enabled = ?", true).Order("id asc").Find(&notifications).Error; err != nil {
		log.Error().Err(err).Msg("获取通知目标失败")
		return
	}

	results := make([]struct {
		notification models.Notification
		result       <-chan error
	}, 0, len(notifications))
	for index := range notifications {
		notification := notifications[index]
		if !notificationHandlesEvent(notification, event) {
			continue
		}
		result := make(chan error, 1)
		notificationJobs <- notificationJob{notification: notification, title: title, message: message, result: result}
		results = append(results, struct {
			notification models.Notification
			result       <-chan error
		}{notification: notification, result: result})
	}
	for _, pending := range results {
		if err := <-pending.result; err != nil {
			log.Error().Str("error", notificationErrorForLog(err)).Uint("notificationID", pending.notification.ID).Str("name", pending.notification.Name).Msg("通知目标发送失败")
		}
	}
}

func notificationHandlesEvent(notification models.Notification, event NotifyEvent) bool {
	switch event {
	case NotifyEventComplete:
		return notification.NotifyOnComplete
	case NotifyEventError:
		return notification.NotifyOnError
	case NotifyEventStop:
		return notification.NotifyOnStop
	case NotifyEventManual:
		return notification.NotifyOnManual
	default:
		return false
	}
}

func sendNotification(notification models.Notification, title, message string) error {
	switch notification.Type {
	case models.NotifyTypeWebhook:
		return sendWebhookNotification(notification.WebhookURL, title, message)
	case models.NotifyTypeTelegram:
		return sendTelegramNotification(notification.TelegramToken, notification.TelegramChatID, title, message)
	default:
		return fmt.Errorf("未知的通知类型")
	}
}

func SendTestNotification(notification models.Notification) error {
	return sendNotification(notification, "CloudStream 测试通知", "这是一条测试通知，说明你的通知配置可用。")
}

const (
	notificationResponseLimit = 64 * 1024
	notificationLogLimit      = 2 * 1024
)

var notificationURLPattern = regexp.MustCompile(`(?i)https?://[^\s"']+`)

var restyClient = resty.New().
	SetTimeout(10 * time.Second).
	SetResponseBodyLimit(notificationResponseLimit)

func sendWebhookNotification(webhookURL, title, message string) error {
	if webhookURL == "" {
		return nil
	}
	payload := map[string]interface{}{
		"msg_type": "post",
		"content": map[string]interface{}{
			"post": map[string]interface{}{
				"zh_cn": map[string]interface{}{
					"title": title,
					"content": [][]map[string]string{{
						{"tag": "text", "text": message},
					}},
				},
			},
		},
	}
	resp, err := restyClient.R().SetHeader("Content-Type", "application/json").SetBody(payload).Post(webhookURL)
	if err != nil || resp == nil || !resp.IsSuccess() {
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode()
		}
		log.Error().Str("error", notificationErrorForLog(err)).Int("statusCode", statusCode).Msg("Webhook通知发送失败")
		if err != nil {
			return fmt.Errorf("webhook 请求失败: %s", notificationErrorForLog(err))
		}
		return fmt.Errorf("webhook 返回状态异常: %d", statusCode)
	}
	return nil
}

func sendTelegramNotification(token, chatID, title, message string) error {
	if token == "" || chatID == "" {
		return nil
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	payload := map[string]string{
		"chat_id": chatID,
		"text":    fmt.Sprintf("%s\n\n%s", title, message),
	}
	resp, err := restyClient.R().SetFormData(payload).Post(url)
	if err != nil || resp == nil || !resp.IsSuccess() {
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode()
		}
		log.Error().Str("error", notificationErrorForLog(err)).Int("statusCode", statusCode).Msg("Telegram通知发送失败")
		if err != nil {
			return fmt.Errorf("telegram 请求失败: %s", notificationErrorForLog(err))
		}
		return fmt.Errorf("telegram 返回状态异常: %d", statusCode)
	}
	return nil
}

func truncateForLog(value string, maxBytes int) string {
	value = strings.ReplaceAll(value, "\r", "\\r")
	value = strings.ReplaceAll(value, "\n", "\\n")
	if len(value) <= maxBytes {
		return value
	}
	return value[:maxBytes] + "...<truncated>"
}

func notificationErrorForLog(err error) string {
	if err == nil {
		return ""
	}
	var urlError *url.Error
	if errors.As(err, &urlError) && urlError.Err != nil {
		err = urlError.Err
	}
	message := notificationURLPattern.ReplaceAllString(err.Error(), "[redacted-url]")
	return truncateForLog(message, notificationLogLimit)
}
