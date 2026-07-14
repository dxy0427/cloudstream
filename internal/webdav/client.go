package webdav

import (
	"cloudstream/internal/models"
	"cloudstream/internal/utils"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/studio-b12/gowebdav"
)

var sharedTransport = &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          100,
	MaxIdleConnsPerHost:   10,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ResponseHeaderTimeout: 30 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
}

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

	username := strings.TrimSpace(account.WebDAVUsername)
	password := strings.TrimSpace(account.WebDAVPassword)
	c := &Client{
		AccountID:           account.ID,
		BaseURL:             base,
		Username:            username,
		Password:            password,
		CacheTTL:            account.CacheTTL,
		CustomCachePolicies: account.CustomCachePolicies,
		HTTPClient:          &http.Client{Transport: sharedTransport},
		MetadataHTTPClient: &http.Client{
			Transport: sharedTransport,
			Timeout:   30 * time.Second,
		},
	}
	c.client = gowebdav.NewClient(base, c.Username, c.Password)
	c.client.SetTransport(sharedTransport)
	c.client.SetTimeout(30 * time.Second)
	return c
}

func (c *Client) DoDownloadRequest(ctx context.Context, httpClient *http.Client, method, filePath string, headers http.Header) (*http.Response, error) {
	if ctx == nil {
		return nil, fmt.Errorf("WebDAV 请求上下文无效")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fileURL, err := c.buildFileURL(filePath)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = c.HTTPClient
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	authorizer := gowebdav.NewAutoAuth(c.Username, c.Password)
	authenticator, _ := authorizer.NewAuthenticator(nil)
	defer authenticator.Close()

	for attempt := 0; attempt < 4; attempt++ {
		request, err := http.NewRequestWithContext(ctx, method, fileURL, nil)
		if err != nil {
			return nil, err
		}
		request.Header = make(http.Header)
		for key, values := range headers {
			request.Header[key] = append([]string(nil), values...)
		}
		authPath := request.URL.RequestURI()
		if err := authenticator.Authorize(httpClient, request, authPath); err != nil {
			return nil, err
		}
		response, err := httpClient.Do(request)
		if err != nil {
			return nil, err
		}
		redo, verifyErr := authenticator.Verify(httpClient, response, authPath)
		if verifyErr != nil {
			response.Body.Close()
			return nil, verifyErr
		}
		if !redo {
			return response, nil
		}
		response.Body.Close()
	}
	return nil, fmt.Errorf("WebDAV 认证重试次数过多")
}

// ListDirectory 列出目录内容，使用 PROPFIND
func (c *Client) ListDirectory(dirPath string) ([]FileInfo, error) {
	return c.ListDirectoryContext(context.Background(), dirPath)
}

func (c *Client) ListDirectoryContext(ctx context.Context, dirPath string) ([]FileInfo, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if dirPath == "" {
		dirPath = "/"
	}
	if !strings.HasPrefix(dirPath, "/") {
		dirPath = "/" + dirPath
	}

	cacheTTL := utils.GetCacheTTL(c.CustomCachePolicies, c.CacheTTL, dirPath)
	cacheKey := fmt.Sprintf("webdav:%d:%s:%s", c.AccountID, c.cacheFingerprint(), dirPath)

	if c.AccountID != 0 && cacheTTL > 0 {
		dirCacheMutex.RLock()
		if item, ok := dirCache[cacheKey]; ok {
			if time.Now().Before(item.ExpiresAt) {
				dirCacheMutex.RUnlock()
				return item.Data, nil
			}
		}
		dirCacheMutex.RUnlock()
	}

	client := c.newContextClient(ctx)
	files, err := client.ReadDir(dirPath)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if c.AccountID != 0 && cacheTTL > 0 {
		dirCacheMutex.Lock()
		dirCache[cacheKey] = &dirCacheEntry{
			Data:      result,
			ExpiresAt: time.Now().Add(time.Duration(cacheTTL) * time.Minute),
		}
		dirCacheMutex.Unlock()
	}

	return result, nil
}

func (c *Client) newContextClient(ctx context.Context) *gowebdav.Client {
	client := gowebdav.NewAuthClient(c.BaseURL, gowebdav.NewAutoAuth(c.Username, c.Password))
	var transport http.RoundTripper = sharedTransport
	var timeout time.Duration = 30 * time.Second
	if c.MetadataHTTPClient != nil {
		transport = c.MetadataHTTPClient.Transport
		timeout = c.MetadataHTTPClient.Timeout
	}
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.SetTransport(transport)
	client.SetTimeout(timeout)
	client.SetInterceptor(func(_ string, req *http.Request) {
		*req = *req.WithContext(ctx)
	})
	return client
}

// GetDownloadURL 获取文件的下载 URL
func (c *Client) GetDownloadURL(filePath string) (string, error) {
	return c.buildFileURL(filePath)
}

// NewDownloadRequest accepts (context.Context, method, path, body) and the
// existing (method, path, body) form used by callers that cannot pass context.
func (c *Client) NewDownloadRequest(args ...any) (*http.Request, error) {
	ctx := context.Background()
	var method string
	var filePath string
	var body io.Reader

	switch len(args) {
	case 3:
		var ok bool
		method, ok = args[0].(string)
		if !ok {
			return nil, fmt.Errorf("WebDAV 请求方法无效")
		}
		filePath, ok = args[1].(string)
		if !ok {
			return nil, fmt.Errorf("WebDAV 文件路径无效")
		}
		if args[2] != nil {
			body, ok = args[2].(io.Reader)
			if !ok {
				return nil, fmt.Errorf("WebDAV 请求体无效")
			}
		}
	case 4:
		var ok bool
		ctx, ok = args[0].(context.Context)
		if !ok || ctx == nil {
			return nil, fmt.Errorf("WebDAV 请求上下文无效")
		}
		method, ok = args[1].(string)
		if !ok {
			return nil, fmt.Errorf("WebDAV 请求方法无效")
		}
		filePath, ok = args[2].(string)
		if !ok {
			return nil, fmt.Errorf("WebDAV 文件路径无效")
		}
		if args[3] != nil {
			body, ok = args[3].(io.Reader)
			if !ok {
				return nil, fmt.Errorf("WebDAV 请求体无效")
			}
		}
	default:
		return nil, fmt.Errorf("WebDAV 下载请求参数无效")
	}

	fileURL, err := c.buildFileURL(filePath)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, fileURL, body)
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

func (c *Client) cacheFingerprint() string {
	h := sha256.New()
	for _, part := range []string{
		fmt.Sprintf("%d", c.AccountID),
		c.BaseURL,
		"",
		"auto",
		c.Username,
		c.Password,
	} {
		_, _ = fmt.Fprintf(h, "%d:", len(part))
		_, _ = h.Write([]byte(part))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// TestConnection 测试 WebDAV 连接
func (c *Client) TestConnection() error {
	return c.TestConnectionContext(context.Background())
}

func (c *Client) TestConnectionContext(ctx context.Context) error {
	testClient := *c
	testClient.AccountID = 0
	testClient.CacheTTL = 0
	testClient.CustomCachePolicies = ""
	_, err := testClient.ListDirectoryContext(ctx, "/")
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
