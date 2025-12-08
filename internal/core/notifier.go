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

// 发送 Webhook 通知 (生产环境使用，读取数据库配置)
func SendNotification(title, content string) {
	var user models.User
	if err := database.DB.First(&user).Error; err != nil {
		return
	}
	if user.WebhookURL == "" {
		return
	}
	go pushToWebhook(user.WebhookURL, title, content)
}

// 发送测试通知 (测试按钮使用，直接使用传入的 URL)
func SendTestNotification(targetURL string) error {
	if targetURL == "" {
		return fmt.Errorf("URL 不能为空")
	}
	return pushToWebhook(targetURL, "CloudStream 测试", "这是一条测试消息，证明 Webhook 配置正确！")
}

// 统一推送逻辑
func pushToWebhook(url, title, content string) error {
	// 构造通用的 JSON 格式
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
		log.Error().Err(err).Str("url", url).Msg("发送通知失败")
		return err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP 状态码错误: %d", resp.StatusCode)
	}
	return nil
}

// 读取最后 N 字节的日志
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
	readSize := int64(50 * 1024) // 50KB
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