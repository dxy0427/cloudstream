package webdav

import (
	"cloudstream/internal/models"
	"cloudstream/internal/utils"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/studio-b12/gowebdav"
)

type Client struct {
	AccountID          uint
	BaseURL            string
	Username           string
	Password           string
	CacheTTL           int
	CustomCachePolicies string
	HTTPClient         *http.Client
	client             *gowebdav.Client
}

type FileInfo struct {
	Name     string
	Size     int64
	IsDir    bool
	Modified time.Time
}

type dirCacheEntry struct {
	Data      []FileInfo
	ExpiresAt time.Time
}

var (
	dirCache      = make(map[string]*dirCacheEntry)
	dirCacheMutex sync.RWMutex
)

func init() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			dirCacheMutex.Lock()
			now := time.Now()
			for k, v := range dirCache {
				if now.After(v.ExpiresAt) {
					delete(dirCache, k)
				}
			}
			dirCacheMutex.Unlock()
		}
	}()
}

func NewClient(account models.Account) *Client {
	base := strings.TrimSpace(account.WebDAVURL)
	if base != "" && !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	base = strings.TrimRight(base, "/")

	c := &Client{
		AccountID:          account.ID,
		BaseURL:            base,
		Username:           strings.TrimSpace(account.WebDAVUsername),
		Password:           strings.TrimSpace(account.WebDAVPassword),
		CacheTTL:           account.CacheTTL,
		CustomCachePolicies: account.CustomCachePolicies,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	c.client = gowebdav.NewClient(base, c.Username, c.Password)
	return c
}


// ListDirectory 列出目录内容，使用 PROPFIND
func (c *Client) ListDirectory(dirPath string) ([]FileInfo, error) {
	if dirPath == "" {
		dirPath = "/"
	}
	if !strings.HasPrefix(dirPath, "/") {
		dirPath = "/" + dirPath
	}

	cacheTTL := utils.GetCacheTTL(c.CustomCachePolicies, c.CacheTTL, dirPath)
	cacheKey := fmt.Sprintf("webdav:%d:%s", c.AccountID, dirPath)

	if cacheTTL > 0 {
		dirCacheMutex.RLock()
		if item, ok := dirCache[cacheKey]; ok {
			if time.Now().Before(item.ExpiresAt) {
				dirCacheMutex.RUnlock()
				return item.Data, nil
			}
		}
		dirCacheMutex.RUnlock()
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

	if cacheTTL > 0 {
		dirCacheMutex.Lock()
		dirCache[cacheKey] = &dirCacheEntry{
			Data:      result,
			ExpiresAt: time.Now().Add(time.Duration(cacheTTL) * time.Minute),
		}
		dirCacheMutex.Unlock()
	}

	return result, nil
}

// GetDownloadURL 获取文件的下载 URL
func (c *Client) GetDownloadURL(filePath string) (string, error) {
	if filePath == "" {
		return "", fmt.Errorf("path 不能为空")
	}
	if !strings.HasPrefix(filePath, "/") {
		filePath = "/" + filePath
	}

	_, err := c.client.Stat(filePath)
	if err != nil {
		return "", fmt.Errorf("WebDAV 文件不存在: %w", err)
	}

	url := c.BaseURL + filePath
	if c.Username != "" && c.Password != "" {
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
