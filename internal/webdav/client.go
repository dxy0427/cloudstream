package webdav

import (
	"cloudstream/internal/models"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/studio-b12/gowebdav"
)

type Client struct {
	AccountID  uint
	BaseURL    string
	Username   string
	Password   string
	HTTPClient *http.Client
	client     *gowebdav.Client
}

func NewClient(account models.Account) *Client {
	base := strings.TrimSpace(account.WebDAVURL)
	if base != "" && !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	base = strings.TrimRight(base, "/")

	c := &Client{
		AccountID: account.ID,
		BaseURL:   base,
		Username:  strings.TrimSpace(account.WebDAVUsername),
		Password:  strings.TrimSpace(account.WebDAVPassword),
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	c.client = gowebdav.NewClient(base, c.Username, c.Password)
	return c
}

type FileInfo struct {
	Name     string
	Size     int64
	IsDir    bool
	Modified time.Time
}

// ListDirectory 列出目录内容，使用 PROPFIND
func (c *Client) ListDirectory(dirPath string) ([]FileInfo, error) {
	if dirPath == "" {
		dirPath = "/"
	}
	if !strings.HasPrefix(dirPath, "/") {
		dirPath = "/" + dirPath
	}

	files, err := c.client.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("WebDAV 列目录失败: %w", err)
	}

	var result []FileInfo
	for _, f := range files {
		result = append(result, FileInfo{
			Name:     f.Name(),
			Size:     f.Size(),
			IsDir:    f.IsDir(),
			Modified: f.ModTime(),
		})
	}
	return result, nil
}

// GetDownloadURL 获取文件的下载 URL
// WebDAV 的 GET 请求就是下载，URL 带 Basic Auth
func (c *Client) GetDownloadURL(filePath string) (string, error) {
	if filePath == "" {
		return "", fmt.Errorf("path 不能为空")
	}
	if !strings.HasPrefix(filePath, "/") {
		filePath = "/" + filePath
	}

	// 验证文件存在
	_, err := c.client.Stat(filePath)
	if err != nil {
		return "", fmt.Errorf("WebDAV 文件不存在: %w", err)
	}

	// 构建带认证的下载 URL
	// 格式: http://user:pass@host/path
	url := c.BaseURL + filePath
	if c.Username != "" && c.Password != "" {
		// 在 URL 中嵌入 Basic Auth（部分播放器支持）
		url = fmt.Sprintf("%s://%s:%s@%s%s",
			getScheme(c.BaseURL),
			c.Username, c.Password,
			getHost(c.BaseURL),
			filePath,
		)
	}

	return url, nil
}

// TestConnection 测试 WebDAV 连接
func (c *Client) TestConnection() error {
	_, err := c.client.ReadDir("/")
	if err != nil {
		return fmt.Errorf("WebDAV 连接失败: %w", err)
	}
	return nil
}

func getScheme(rawURL string) string {
	if strings.HasPrefix(rawURL, "https://") {
		return "https"
	}
	return "http"
}

func getHost(rawURL string) string {
	s := rawURL
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "https://")
	if idx := strings.Index(s, "/"); idx >= 0 {
		return s[:idx]
	}
	return s
}
