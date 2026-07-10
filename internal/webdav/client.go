package webdav

import (
	"cloudstream/internal/models"
	"cloudstream/internal/utils"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/studio-b12/gowebdav"
)

type Client struct {
	AccountID           uint
	BaseURL             string
	Username            string
	Password            string
	CacheTTL            int
	CustomCachePolicies string
	HTTPClient          *http.Client
	MetadataHTTPClient  *http.Client
	client              *gowebdav.Client
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
		AccountID:           account.ID,
		BaseURL:             base,
		Username:            strings.TrimSpace(account.WebDAVUsername),
		Password:            strings.TrimSpace(account.WebDAVPassword),
		CacheTTL:            account.CacheTTL,
		CustomCachePolicies: account.CustomCachePolicies,
		HTTPClient:          &http.Client{},
		MetadataHTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	c.client = gowebdav.NewClient(base, c.Username, c.Password)
	c.client.SetTimeout(30 * time.Second)
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
	return c.buildFileURL(filePath)
}

func (c *Client) NewDownloadRequest(method, filePath string, body io.Reader) (*http.Request, error) {
	fileURL, err := c.buildFileURL(filePath)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(method, fileURL, body)
	if err != nil {
		return nil, err
	}
	if c.Username != "" || c.Password != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}
	return req, nil
}

func (c *Client) buildFileURL(filePath string) (string, error) {
	if filePath == "" {
		return "", fmt.Errorf("path 不能为空")
	}
	baseURL, err := url.Parse(c.BaseURL)
	if err != nil {
		return "", fmt.Errorf("WebDAV 地址无效: %w", err)
	}
	cleanPath := path.Clean("/" + strings.TrimLeft(filePath, "/"))
	if cleanPath == "/" {
		return "", fmt.Errorf("path 必须指向文件")
	}
	baseURL.RawPath = ""
	baseURL.Path = path.Join(baseURL.Path, strings.TrimPrefix(cleanPath, "/"))

	return baseURL.String(), nil
}

// TestConnection 测试 WebDAV 连接
func (c *Client) TestConnection() error {
	_, err := c.client.ReadDir("/")
	if err != nil {
		return fmt.Errorf("WebDAV 连接失败: %w", err)
	}
	return nil
}

func InvalidateAccountCache(accountID uint) {
	prefix := fmt.Sprintf("webdav:%d:", accountID)
	dirCacheMutex.Lock()
	for key := range dirCache {
		if strings.HasPrefix(key, prefix) {
			delete(dirCache, key)
		}
	}
	dirCacheMutex.Unlock()
}
