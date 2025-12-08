package core

import (
	"bytes"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog/log"
	"io"
	"net/http"
	"os"
	"time"
)

// 发送通知 (生产环境)
func SendNotification(title, content string) {
	var user models.User
	if err := database.DB.First(&user).Error; err != nil {
		return
	}

	if user.NotifyType == models.NotifyTypeTelegram {
		if user.TelegramToken != "" && user.TelegramChatID != "" {
			go pushToTelegram(user.TelegramToken, user.TelegramChatID, fmt.Sprintf("*%s*\n%s", title, content))
		}
	} else {
		// 默认 Webhook
		if user.WebhookURL != "" {
			go pushToWebhook(user.WebhookURL, title, content)
		}
	}
}

// 发送测试通知
func SendTestNotification(req map[string]string) error {
	nType := req["type"]
	if nType == models.NotifyTypeTelegram {
		token := req["token"]
		chatID := req["chatId"]
		if token == "" || chatID == "" {
			return fmt.Errorf("Telegram Token 和 ChatID 不能为空")
		}
		return pushToTelegram(token, chatID, "*CloudStream 测试*\n通知服务配置成功！")
	} else {
		url := req["url"]
		if url == "" {
			return fmt.Errorf("Webhook URL 不能为空")
		}
		return pushToWebhook(url, "CloudStream 测试", "通知服务配置成功！")
	}
}

// 通用 Webhook 推送
func pushToWebhook(url, title, content string) error {
	payload := map[string]string{
		"title":   title,
		"body":    content,
		"content": content,
		"msg":     content,
	}
	data, _ := json.Marshal(payload)

	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		log.Error().Err(err).Str("url", url).Msg("Webhook 发送失败")
		return err
	}
	defer resp.Body.Close()
	return nil
}

// Telegram 推送
func pushToTelegram(token, chatID, text string) error {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	payload := map[string]string{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}
	data, _ := json.Marshal(payload)

	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(apiURL, "application/json", bytes.NewBuffer(data))
	if err != nil {
		log.Error().Err(err).Msg("Telegram 发送失败")
		return err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Telegram API 错误: %s", string(body))
	}
	return nil
}

// 读取日志
func ReadRecentLogs() (string, error) {
	logPath := "./data/cloudstream.log"
	file, err := os.Open(logPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return "", err
	}

	fileSize := stat.Size()
	readSize := int64(50 * 1024) 
	if fileSize < readSize {
		readSize = fileSize
	}

	offset := fileSize - readSize
	buffer := make([]byte, readSize)
	
	_, err = file.ReadAt(buffer, offset)
	if err != nil && err != io.EOF {
		return "", err
	}

	return string(buffer), nil
}