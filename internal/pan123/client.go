package pan123

import (
	"bytes"
	"cloudstream/internal/models"
	"cloudstream/internal/utils"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	ApiBaseURL = "https://open-api.123pan.com"
	Timeout    = 60 * time.Second
	UserAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

	maxControlResponseBodySize = int64(4 << 20)
)

// --- 缓存结构 ---
type tokenCacheItem struct {
	mu        sync.RWMutex
	Token     string
	ExpiresAt time.Time
}

type listCacheItem struct {
	Data       []FileInfo
	NextFileId int64
	ExpiresAt  time.Time
}

var (
	tokenCaches       = make(map[string]*tokenCacheItem)
	tokenRefreshLocks = make(map[uint]chan struct{})
	mapMutex          sync.Mutex

	listCache      = make(map[string]*listCacheItem)
	listCacheMutex sync.RWMutex
)

func init() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			listCacheMutex.Lock()
			now := time.Now()
			for k, v := range listCache {
				if now.After(v.ExpiresAt) {
					delete(listCache, k)
				}
			}
			listCacheMutex.Unlock()

			mapMutex.Lock()
			for k, v := range tokenCaches {
				v.mu.RLock()
				expired := v.Token != "" && now.After(v.ExpiresAt)
				v.mu.RUnlock()
				if expired {
					delete(tokenCaches, k)
				}
			}
			mapMutex.Unlock()
		}
	}()
}

type Client struct {
	HTTPClient *http.Client
	Account    models.Account
}

func NewClient(account models.Account) *Client {
	return &Client{
		HTTPClient: &http.Client{Timeout: Timeout},
		Account:    account,
	}
}

func (c *Client) getAccessToken() (string, error) {
	return c.getAccessTokenContext(context.Background(), c.cacheFingerprint())
}

func (c *Client) getAccessTokenContext(ctx context.Context, fingerprint string) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if c.Account.ID == 0 {
		token, _, err := c.requestAccessTokenContext(ctx)
		return token, err
	}

	cacheKey := c.tokenCacheKey(fingerprint)
	mapMutex.Lock()
	if _, ok := tokenCaches[cacheKey]; !ok {
		tokenCaches[cacheKey] = &tokenCacheItem{}
	}
	cache := tokenCaches[cacheKey]
	refresh, ok := tokenRefreshLocks[c.Account.ID]
	if !ok {
		refresh = make(chan struct{}, 1)
		tokenRefreshLocks[c.Account.ID] = refresh
	}
	mapMutex.Unlock()

	cache.mu.RLock()
	if cache.Token != "" && time.Now().Before(cache.ExpiresAt.Add(-5*time.Minute)) {
		token := cache.Token
		cache.mu.RUnlock()
		return token, nil
	}
	cache.mu.RUnlock()

	select {
	case refresh <- struct{}{}:
		defer func() { <-refresh }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	mapMutex.Lock()
	if _, ok := tokenCaches[cacheKey]; !ok {
		tokenCaches[cacheKey] = &tokenCacheItem{}
	}
	cache = tokenCaches[cacheKey]
	mapMutex.Unlock()

	cache.mu.RLock()
	if cache.Token != "" && time.Now().Before(cache.ExpiresAt.Add(-5*time.Minute)) {
		token := cache.Token
		cache.mu.RUnlock()
		return token, nil
	}
	cache.mu.RUnlock()

	token, expires, err := c.requestAccessTokenContext(ctx)
	if err != nil {
		return "", err
	}

	cache.mu.Lock()
	cache.Token = token
	cache.ExpiresAt = expires
	cache.mu.Unlock()
	mapMutex.Lock()
	tokenCaches[cacheKey] = cache
	mapMutex.Unlock()
	log.Info().Str("account", c.Account.Name).Msg("AccessToken 已成功刷新")

	return token, nil
}

func (c *Client) requestAccessToken() (string, time.Time, error) {
	return c.requestAccessTokenContext(context.Background())
}

func (c *Client) requestAccessTokenContext(ctx context.Context) (string, time.Time, error) {
	if ctx == nil {
		return "", time.Time{}, fmt.Errorf("context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return "", time.Time{}, err
	}

	apiURL := ApiBaseURL + "/api/v1/access_token"
	bodyData, _ := json.Marshal(map[string]string{
		"client_id":     c.Account.ClientID,
		"client_secret": c.Account.ClientSecret,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewBuffer(bodyData))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("创建 AccessToken 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("platform", "open_platform")
	req.Header.Set("User-Agent", UserAgent)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", time.Time{}, ctx.Err()
		}
		return "", time.Time{}, fmt.Errorf("请求 AccessToken 失败: %w", err)
	}
	defer resp.Body.Close()
	bodyBytes, err := readControlResponseBody(resp.Body)
	if err != nil {
		if ctx.Err() != nil {
			return "", time.Time{}, ctx.Err()
		}
		return "", time.Time{}, fmt.Errorf("读取 AccessToken 响应失败: %w", err)
	}

	var tokenResp AccessTokenResp
	if err := json.Unmarshal(bodyBytes, &tokenResp); err != nil {
		return "", time.Time{}, fmt.Errorf("解析 AccessToken 响应失败: %w", err)
	}
	if tokenResp.Code != 0 {
		return "", time.Time{}, fmt.Errorf("获取 AccessToken API 错误 (code: %d): %s", tokenResp.Code, tokenResp.Message)
	}
	if tokenResp.Data.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("API 未返回有效的 AccessToken")
	}

	expires, err := time.Parse(time.RFC3339, tokenResp.Data.ExpiredAt)
	if err != nil {
		expires, err = time.Parse("2006-01-02 15:04:05", tokenResp.Data.ExpiredAt)
		if err != nil {
			return "", time.Time{}, fmt.Errorf("解析 Token 过期时间失败: %w", err)
		}
	}

	return tokenResp.Data.AccessToken, expires, nil
}

func (c *Client) invalidateToken() {
	c.invalidateTokenFingerprint(c.cacheFingerprint())
}

func (c *Client) invalidateTokenFingerprint(fingerprint string) {
	if c.Account.ID == 0 {
		return
	}
	mapMutex.Lock()
	delete(tokenCaches, c.tokenCacheKey(fingerprint))
	mapMutex.Unlock()
	log.Warn().Str("account", c.Account.Name).Msg("123Pan Token 被标记失效，准备重新获取")
}

func (c *Client) sendAuthorizedRequest(method, endpoint string, queryParams map[string]interface{}) (json.RawMessage, error) {
	return c.sendAuthorizedRequestContext(context.Background(), method, endpoint, queryParams, c.cacheFingerprint())
}

func (c *Client) sendAuthorizedRequestContext(ctx context.Context, method, endpoint string, queryParams map[string]interface{}, fingerprint string) (json.RawMessage, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	fullURL, err := url.Parse(ApiBaseURL)
	if err != nil {
		return nil, fmt.Errorf("解析 123Pan API 地址失败: %w", err)
	}
	fullURL.Path = endpoint
	q := fullURL.Query()
	if queryParams != nil {
		for k, v := range queryParams {
			q.Set(k, fmt.Sprintf("%v", v))
		}
	}
	fullURL.RawQuery = q.Encode()

	// 鉴权重试：最多2次
	maxAuthRetries := 2
	for authAttempt := 0; authAttempt < maxAuthRetries; authAttempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		accessToken, err := c.getAccessTokenContext(ctx, fingerprint)
		if err != nil {
			return nil, err
		}

		// 网络重试：最多3次
		maxNetRetries := 3
		var lastNetErr error
		var resp *http.Response
		var bodyBytes []byte

		for i := 0; i < maxNetRetries; i++ {
			if i > 0 {
				if err := sleepContext(ctx, time.Duration(1<<uint(i-1))*time.Second); err != nil {
					return nil, err
				}
			}

			req, err := http.NewRequestWithContext(ctx, method, fullURL.String(), nil)
			if err != nil {
				return nil, fmt.Errorf("创建请求失败: %w", err)
			}
			req.Header.Set("Authorization", "Bearer "+accessToken)
			req.Header.Set("platform", "open_platform")
			req.Header.Set("User-Agent", UserAgent)

			resp, err = c.HTTPClient.Do(req)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				lastNetErr = err
				continue
			}

			bodyBytes, err = readControlResponseBody(resp.Body)
			resp.Body.Close()
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				lastNetErr = err
				continue
			}
			lastNetErr = nil
			break
		}

		if lastNetErr != nil {
			return nil, fmt.Errorf("请求 123Pan 失败(重试%d次): %w", maxNetRetries, lastNetErr)
		}

		if resp.StatusCode == 401 {
			if authAttempt == 0 {
				c.invalidateTokenFingerprint(fingerprint)
				continue
			}
			return nil, fmt.Errorf("123Pan 鉴权失败 (HTTP 401)")
		}

		var result struct {
			BaseResp
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			return nil, fmt.Errorf("解析 JSON 失败: %w", err)
		}

		if result.Code == 429 {
			if authAttempt == 0 {
				if err := sleepContext(ctx, 3*time.Second); err != nil {
					return nil, err
				}
				continue
			}
			return nil, fmt.Errorf("123Pan 频率限制 (Code 429)，重试次数耗尽")
		}

		if result.Code == 401 {
			if authAttempt == 0 {
				c.invalidateTokenFingerprint(fingerprint)
				continue
			}
			return nil, fmt.Errorf("123Pan Token 失效 (Code 401)")
		}

		if result.Code != 0 {
			return nil, fmt.Errorf("123Pan API 错误 (code: %d): %s", result.Code, result.Message)
		}

		return result.Data, nil
	}

	return nil, fmt.Errorf("123Pan 请求失败：鉴权重试次数耗尽")
}

func (c *Client) ListFiles(parentFileId int64, limit int, lastFileId int64, parentPath string) ([]FileInfo, int64, error) {
	return c.ListFilesContext(context.Background(), parentFileId, limit, lastFileId, parentPath)
}

func (c *Client) ListFilesContext(ctx context.Context, parentFileId int64, limit int, lastFileId int64, parentPath string) ([]FileInfo, int64, error) {
	if ctx == nil {
		return nil, 0, fmt.Errorf("context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}

	if c.Account.Type == models.AccountType123Pan {
		fingerprint := c.cacheFingerprint()
		cacheKey := c.listCacheKey(fingerprint, parentFileId, lastFileId)
		cacheTTL := utils.GetCacheTTL(c.Account.CustomCachePolicies, c.Account.CacheTTL, parentPath)

		if c.Account.ID != 0 && cacheTTL > 0 {
			listCacheMutex.RLock()
			if item, ok := listCache[cacheKey]; ok {
				if time.Now().Before(item.ExpiresAt) {
					listCacheMutex.RUnlock()
					return item.Data, item.NextFileId, nil
				}
			}
			listCacheMutex.RUnlock()
		}

		params := map[string]interface{}{
			"parentFileId":   parentFileId,
			"limit":          limit,
			"trashed":        0,
			"orderBy":        "fileId",
			"orderDirection": "asc",
		}
		if lastFileId > 0 {
			params["lastFileId"] = lastFileId
		}

		rawData, err := c.sendAuthorizedRequestContext(ctx, http.MethodGet, "/api/v2/file/list", params, fingerprint)
		if err != nil {
			return nil, 0, err
		}
		var listData struct {
			FileList   []FileInfo `json:"fileList"`
			LastFileId int64      `json:"lastFileId"`
		}
		if err := json.Unmarshal(rawData, &listData); err != nil {
			return nil, 0, fmt.Errorf("解析文件列表数据失败: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}

		if c.Account.ID != 0 && cacheTTL > 0 {
			listCacheMutex.Lock()
			listCache[cacheKey] = &listCacheItem{
				Data:       listData.FileList,
				NextFileId: listData.LastFileId,
				ExpiresAt:  time.Now().Add(time.Duration(cacheTTL) * time.Minute),
			}
			listCacheMutex.Unlock()
		}

		return listData.FileList, listData.LastFileId, nil
	}
	return []FileInfo{}, -1, nil
}

func (c *Client) GetDownloadURL(identifier interface{}) (string, error) {
	return c.GetDownloadURLContext(context.Background(), identifier)
}

func (c *Client) GetDownloadURLContext(ctx context.Context, identifier interface{}) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	var fileID int64
	switch v := identifier.(type) {
	case int64:
		fileID = v
	case string:
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			fileID = id
		} else {
			return "", fmt.Errorf("无效的 FileID: %s", v)
		}
	default:
		return "", fmt.Errorf("123Pan ID 类型错误")
	}

	params := map[string]interface{}{"fileId": strconv.FormatInt(fileID, 10)}
	rawData, err := c.sendAuthorizedRequestContext(ctx, http.MethodGet, "/api/v1/file/download_info", params, c.cacheFingerprint())
	if err != nil {
		return "", err
	}
	var downloadInfo struct {
		DownloadURL string `json:"downloadUrl"`
	}
	if err := json.Unmarshal(rawData, &downloadInfo); err != nil {
		return "", fmt.Errorf("解析下载链接失败: %w", err)
	}
	if downloadInfo.DownloadURL == "" {
		return "", fmt.Errorf("API 未返回下载链接")
	}
	return downloadInfo.DownloadURL, nil
}

func (c *Client) GetAccessTokenForTest() (string, error) {
	return c.GetAccessTokenForTestContext(context.Background())
}

func (c *Client) GetAccessTokenForTestContext(ctx context.Context) (string, error) {
	testClient := *c
	testClient.Account.ID = 0
	return testClient.getAccessTokenContext(ctx, testClient.cacheFingerprint())
}

func readControlResponseBody(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxControlResponseBodySize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxControlResponseBodySize {
		return nil, fmt.Errorf("响应超过 %d 字节限制", maxControlResponseBodySize)
	}
	return data, nil
}

func (c *Client) cacheFingerprint() string {
	h := sha256.New()
	for _, part := range []string{
		fmt.Sprintf("%d", c.Account.ID),
		ApiBaseURL,
		c.Account.ClientID,
		"client_credentials",
		"",
		c.Account.ClientSecret,
	} {
		_, _ = fmt.Fprintf(h, "%d:", len(part))
		_, _ = h.Write([]byte(part))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func (c *Client) tokenCacheKey(fingerprint string) string {
	return fmt.Sprintf("token:%d:%s", c.Account.ID, fingerprint)
}

func (c *Client) listCacheKey(fingerprint string, parentFileID, lastFileID int64) string {
	h := sha256.New()
	for _, part := range []string{
		fingerprint,
		fmt.Sprintf("%d", c.Account.CacheTTL),
		c.Account.CustomCachePolicies,
		fmt.Sprintf("%d", parentFileID),
		fmt.Sprintf("%d", lastFileID),
	} {
		_, _ = fmt.Fprintf(h, "%d:", len(part))
		_, _ = h.Write([]byte(part))
	}
	return fmt.Sprintf("list:%d:%x", c.Account.ID, h.Sum(nil))
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func InvalidateAccountCache(accountID uint) {
	tokenPrefix := fmt.Sprintf("token:%d:", accountID)
	mapMutex.Lock()
	for key := range tokenCaches {
		if strings.HasPrefix(key, tokenPrefix) {
			delete(tokenCaches, key)
		}
	}
	delete(tokenRefreshLocks, accountID)
	mapMutex.Unlock()

	prefix := fmt.Sprintf("list:%d:", accountID)
	listCacheMutex.Lock()
	for key := range listCache {
		if strings.HasPrefix(key, prefix) {
			delete(listCache, key)
		}
	}
	listCacheMutex.Unlock()
}
