package core

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"errors"
	"fmt"
	"net/url"
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

func SendNotificationByEvent(title, message string, event NotifyEvent) {
	sendNotificationWithEvent(title, message, event)
}

func sendNotificationWithEvent(title, message string, event NotifyEvent) {
	var user models.User
	if err := database.DB.First(&user).Error; err != nil {
		log.Error().Err(err).Msg("获取通知配置失败")
		return
	}

	switch event {
	case NotifyEventComplete:
		if !user.NotifyOnComplete {
			return
		}
	case NotifyEventError:
		if !user.NotifyOnError {
			return
		}
	case NotifyEventStop:
		if !user.NotifyOnStop {
			return
		}
	case NotifyEventManual:
		if !user.NotifyOnManual {
			return
		}
	}

	switch user.NotifyType {
	case models.NotifyTypeWebhook:
		_ = sendWebhookNotification(user.WebhookURL, title, message)
	case models.NotifyTypeTelegram:
		_ = sendTelegramNotification(user.TelegramToken, user.TelegramChatID, title, message)
	default:
		log.Warn().Str("type", user.NotifyType).Msg("未知的通知类型")
	}
}

func SendTestNotification(req map[string]string) error {
	notifyType := req["notifyType"]
	if notifyType == "" {
		notifyType = models.NotifyTypeWebhook
	}

	switch notifyType {
	case models.NotifyTypeWebhook:
		webhookURL := req["webhookUrl"]
		if webhookURL == "" {
			return fmt.Errorf("Webhook URL 不能为空")
		}
		return sendWebhookNotification(webhookURL, "CloudStream 测试通知", "这是一条测试通知，说明你的 Webhook 配置可用。")
	case models.NotifyTypeTelegram:
		token := req["telegramToken"]
		chatID := req["telegramChatId"]
		if token == "" || chatID == "" {
			return fmt.Errorf("Telegram Token 和 Chat ID 不能为空")
		}
		return sendTelegramNotification(token, chatID, "CloudStream 测试通知", "这是一条测试通知，说明你的 Telegram 配置可用。")
	default:
		return fmt.Errorf("未知的通知类型")
	}
}

const (
	notificationResponseLimit = 64 * 1024
	notificationLogLimit      = 2 * 1024
)

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
		bodyString := ""
		if resp != nil {
			statusCode = resp.StatusCode()
			bodyString = truncateForLog(resp.String(), notificationLogLimit)
		}
		log.Error().Str("error", notificationErrorForLog(err)).Int("statusCode", statusCode).Str("response", bodyString).Msg("Webhook通知发送失败")
		if err != nil {
			return err
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
		"chat_id":    chatID,
		"text":       fmt.Sprintf("*%s*\n\n%s", title, message),
		"parse_mode": "Markdown",
	}
	resp, err := restyClient.R().SetFormData(payload).Post(url)
	if err != nil || resp == nil || !resp.IsSuccess() {
		statusCode := 0
		bodyString := ""
		if resp != nil {
			statusCode = resp.StatusCode()
			bodyString = truncateForLog(resp.String(), notificationLogLimit)
		}
		log.Error().Str("error", notificationErrorForLog(err)).Int("statusCode", statusCode).Str("response", bodyString).Msg("Telegram通知发送失败")
		if err != nil {
			return err
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
	return truncateForLog(err.Error(), notificationLogLimit)
}
