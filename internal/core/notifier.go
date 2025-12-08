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

// 发送通知
func SendNotification(title, content string) {
	var user models.User
	// 获取第一个用户（管理员）的配置
	if err := database.DB.First(&user).Error; err != nil {
		return
	}
	if user.WebhookURL == "" {
		return
	}

	// 构造通用的 JSON 格式 (适配大多数 Webhook，如 Bark, 企业微信等)
	// 如果是 Bark: { "title": "...", "body": "..." }
	// 这里做一个简单的通用 payload
	payload := map[string]string{
		"title":   title,
		"body":    content,
		"content": content, // 兼容部分服务
		"msg":     content, // 兼容部分服务
	}
	data, _ := json.Marshal(payload)

	go func() {
		resp, err := http.Post(user.WebhookURL, "application/json", bytes.NewBuffer(data))
		if err != nil {
			log.Error().Err(err).Msg("发送通知失败")
			return
		}
		defer resp.Body.Close()
	}()
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
	readSize := int64(20480) // 读取最后 20KB
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