package openlist

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
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog/log"
)

const (
	openListPageSize           = 500
	maxControlResponseBodySize = int64(4 << 20)
)

type tokenCacheItem struct {
	mu        sync.RWMutex
	Token     string
	ExpiresAt time.Time
}

var (
	globalTokenCache  = make(map[string]*tokenCacheItem)
	tokenRefreshLocks = make(map[uint]chan struct{})
	cacheMutex        sync.RWMutex
)

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
			now := time.Now()
			cacheMutex.Lock()
			for key, item := range globalTokenCache {
				item.mu.RLock()
				expired := item.ExpiresAt.IsZero() || !now.Before(item.ExpiresAt)
				item.mu.RUnlock()
				if expired {
					delete(globalTokenCache, key)
				}
			}
			cacheMutex.Unlock()

			dirCacheMutex.Lock()
			for k, v := range dirCache {
				if now.After(v.ExpiresAt) {
					delete(dirCache, k)
				}
			}
			dirCacheMutex.Unlock()
		}
	}()
}

type Client struct {
	AccountID           uint
	BaseURL             string
	AuthMode            string
	StaticToken         string
	Username            string
	Password            string
	CacheTTL            int
	CustomCachePolicies string
	HTTPClient          *http.Client
}

func NewClient(account models.Account) *Client {
	base := strings.TrimSpace(account.OpenListURL)
	if base != "" && !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	base = strings.TrimRight(base, "/")
	authMode := strings.TrimSpace(account.OpenListAuthMode)
	if authMode != "token" && authMode != "password" {
		if strings.TrimSpace(account.OpenListToken) != "" {
			authMode = "token"
		} else {
			authMode = "password"
		}
	}

	client := &Client{
		AccountID:           account.ID,
		BaseURL:             base,
		AuthMode:            authMode,
		CacheTTL:            account.CacheTTL,
		CustomCachePolicies: account.CustomCachePolicies,
		HTTPClient:          &http.Client{Timeout: 30 * time.Second},
	}

	switch authMode {
	case "token":
		client.StaticToken = strings.TrimSpace(account.OpenListToken)
	case "password":
		client.Username = strings.TrimSpace(account.OpenListUsername)
		client.Password = strings.TrimSpace(account.OpenListPassword)
	}

	return client
}

// 获取有效 Token (带自动登录、JWT解析和缓存)
func (c *Client) getToken() (string, error) {
	return c.getTokenContext(context.Background(), c.cacheFingerprint())
}

func (c *Client) getTokenContext(ctx context.Context, fingerprint string) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	switch c.AuthMode {
	case "token":
		if c.StaticToken == "" {
			return "", fmt.Errorf("OpenList Token 未配置")
		}
		return c.StaticToken, nil
	case "password":
		if c.Username == "" || c.Password == "" {
			return "", fmt.Errorf("OpenList 用户名或密码未配置")
		}
	default:
		return "", fmt.Errorf("OpenList 认证模式无效")
	}

	if c.AccountID == 0 {
		return c.loginContext(ctx)
	}

	cacheKey := c.tokenCacheKey(fingerprint)
	cacheMutex.Lock()
	item, exists := globalTokenCache[cacheKey]
	if !exists {
		item = &tokenCacheItem{}
		globalTokenCache[cacheKey] = item
	}
	refresh, exists := tokenRefreshLocks[c.AccountID]
	if !exists {
		refresh = make(chan struct{}, 1)
		tokenRefreshLocks[c.AccountID] = refresh
	}
	cacheMutex.Unlock()

	item.mu.RLock()
	if item.Token != "" && time.Now().Before(item.ExpiresAt.Add(-5*time.Minute)) {
		token := item.Token
		item.mu.RUnlock()
		return token, nil
	}
	item.mu.RUnlock()

	select {
	case refresh <- struct{}{}:
		defer func() { <-refresh }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	cacheMutex.Lock()
	item, exists = globalTokenCache[cacheKey]
	if !exists {
		item = &tokenCacheItem{}
		globalTokenCache[cacheKey] = item
	}
	cacheMutex.Unlock()

	item.mu.RLock()
	if item.Token != "" && time.Now().Before(item.ExpiresAt.Add(-5*time.Minute)) {
		token := item.Token
		item.mu.RUnlock()
		return token, nil
	}
	item.mu.RUnlock()

	token, err := c.loginContext(ctx)
	if err != nil {
		return "", err
	}

	expiration := time.Now().Add(24 * time.Hour)
	parsedToken, _, err := jwt.NewParser().ParseUnverified(token, jwt.MapClaims{})
	if err == nil {
		if claims, ok := parsedToken.Claims.(jwt.MapClaims); ok {
			if exp, ok := claims["exp"].(float64); ok {
				realExp := time.Unix(int64(exp), 0)
				if realExp.After(time.Now()) {
					expiration = realExp
					log.Info().Uint("accountID", c.AccountID).Time("expireAt", expiration).Msg("OpenList Token 有效期已自动同步")
				}
			}
		}
	} else {
		log.Warn().Err(err).Msg("解析 OpenList JWT 失败，将使用默认过期时间")
	}

	cacheMutex.Lock()
	currentItem, exists := globalTokenCache[cacheKey]
	if exists && currentItem == item {
		item.mu.Lock()
		item.Token = token
		item.ExpiresAt = expiration
		item.mu.Unlock()
	}
	cacheMutex.Unlock()

	return token, nil
}

func (c *Client) invalidateCache() {
	c.invalidateCacheFingerprint(c.cacheFingerprint())
}

func (c *Client) invalidateCacheFingerprint(fingerprint string) {
	if c.AccountID == 0 {
		return
	}
	cacheMutex.Lock()
	delete(globalTokenCache, c.tokenCacheKey(fingerprint))
	cacheMutex.Unlock()
	log.Warn().Uint("accountID", c.AccountID).Msg("OpenList Token 已被标记为失效，下次请求将重新登录")
}

func (c *Client) login() (string, error) {
	return c.loginContext(context.Background())
}

func (c *Client) loginContext(ctx context.Context) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	apiPath := "/api/auth/login"
	body := map[string]string{
		"username": c.Username,
		"password": c.Password,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+apiPath, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("登录请求失败: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := readControlResponseBody(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取登录响应失败: %w", err)
	}

	var res struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Token string `json:"token"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &res); err != nil {
		return "", fmt.Errorf("解析登录响应失败: %w", err)
	}

	if res.Code != 200 {
		return "", fmt.Errorf("登录失败(code=%d): %s", res.Code, res.Message)
	}

	return res.Data.Token, nil
}

func (c *Client) doPostJSON(apiPath string, body any, out any) error {
	return c.doPostJSONContext(context.Background(), apiPath, body, out, c.cacheFingerprint())
}

func (c *Client) doPostJSONContext(ctx context.Context, apiPath string, body any, out any, fingerprint string) error {
	if ctx == nil {
		return fmt.Errorf("context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.BaseURL == "" {
		return fmt.Errorf("OpenList 地址未配置")
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("编码请求失败: %w", err)
	}

	maxAuthRetries := 2
	for authAttempt := 0; authAttempt < maxAuthRetries; authAttempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		token, err := c.getTokenContext(ctx, fingerprint)
		if err != nil {
			return err
		}

		maxNetRetries := 3
		var lastNetErr error
		var resp *http.Response
		var respBody []byte

		for i := 0; i < maxNetRetries; i++ {
			if i > 0 {
				if err := sleepContext(ctx, time.Duration(1<<uint(i-1))*time.Second); err != nil {
					return err
				}
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+apiPath, bytes.NewReader(jsonData))
			if err != nil {
				return err
			}

			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", token)
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

			resp, err = c.HTTPClient.Do(req)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				lastNetErr = err
				continue
			}

			respBody, err = readControlResponseBody(resp.Body)
			resp.Body.Close()
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				lastNetErr = err
				continue
			}

			lastNetErr = nil
			break
		}

		if lastNetErr != nil {
			return fmt.Errorf("请求 OpenList 网络失败: %w", lastNetErr)
		}

		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			if authAttempt == 0 && c.AuthMode == "password" {
				c.invalidateCacheFingerprint(fingerprint)
				continue
			}
			return fmt.Errorf("OpenList 鉴权失败 (HTTP %d)", resp.StatusCode)
		}

		// OpenList 有时 HTTP 200 但 body 里 code 是错误码
		var tempRes struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(respBody, &tempRes); err == nil {
			if tempRes.Code == 401 {
				if authAttempt == 0 && c.AuthMode == "password" {
					c.invalidateCacheFingerprint(fingerprint)
					continue
				}
				return fmt.Errorf("OpenList 鉴权失败 (Business Code 401)")
			}
		}

		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("解析响应失败: %w, status: %d", err, resp.StatusCode)
		}

		return nil
	}

	return fmt.Errorf("OpenList 请求失败：重试次数耗尽")
}

func (c *Client) ListDirectory(pathStr string, refresh bool) ([]FileInfo, error) {
	return c.ListDirectoryContext(context.Background(), pathStr, refresh)
}

func (c *Client) ListDirectoryContext(ctx context.Context, pathStr string, refresh bool) ([]FileInfo, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if pathStr == "" {
		pathStr = "/"
	}
	if !strings.HasPrefix(pathStr, "/") {
		pathStr = "/" + pathStr
	}

	fingerprint := c.cacheFingerprint()
	cacheTTL := utils.GetCacheTTL(c.CustomCachePolicies, c.CacheTTL, pathStr)
	cacheKey := c.directoryCacheKey(fingerprint, pathStr)

	if c.AccountID != 0 && cacheTTL > 0 && !refresh {
		dirCacheMutex.RLock()
		if item, ok := dirCache[cacheKey]; ok {
			if time.Now().Before(item.ExpiresAt) {
				dirCacheMutex.RUnlock()
				return item.Data, nil
			}
		}
		dirCacheMutex.RUnlock()
	}

	content := make([]FileInfo, 0)
	expectedTotal := -1
	for page := 1; ; page++ {
		body := map[string]any{
			"path":     pathStr,
			"password": "",
			"page":     page,
			"per_page": openListPageSize,
			"refresh":  refresh,
		}

		var res listResponse
		if err := c.doPostJSONContext(ctx, "/api/fs/list", body, &res, fingerprint); err != nil {
			return nil, err
		}
		if res.Code != 200 {
			return nil, fmt.Errorf("OpenList 列表失败(code=%d): %s", res.Code, res.Message)
		}

		if expectedTotal < 0 || res.Data.Total > expectedTotal {
			expectedTotal = res.Data.Total
		}
		pageContent := res.Data.Content
		content = append(content, pageContent...)

		if expectedTotal > 0 {
			if len(content) >= expectedTotal {
				break
			}
			if len(pageContent) == 0 {
				return nil, fmt.Errorf("OpenList 列表分页不完整: 已获取 %d/%d 项", len(content), expectedTotal)
			}
			continue
		}
		if len(pageContent) < openListPageSize {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if c.AccountID != 0 && cacheTTL > 0 {
		dirCacheMutex.Lock()
		dirCache[cacheKey] = &dirCacheEntry{
			Data:      content,
			ExpiresAt: time.Now().Add(time.Duration(cacheTTL) * time.Minute),
		}
		dirCacheMutex.Unlock()
	}

	return content, nil
}

func (c *Client) GetRawURL(pathStr string) (string, error) {
	return c.GetRawURLContext(context.Background(), pathStr)
}

func (c *Client) GetRawURLContext(ctx context.Context, pathStr string) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if pathStr == "" {
		return "", fmt.Errorf("path 不能为空")
	}
	if !strings.HasPrefix(pathStr, "/") {
		pathStr = "/" + pathStr
	}

	body := map[string]any{
		"path":     pathStr,
		"password": "",
	}

	var res getResponse
	if err := c.doPostJSONContext(ctx, "/api/fs/get", body, &res, c.cacheFingerprint()); err != nil {
		return "", err
	}
	if res.Code != 200 {
		return "", fmt.Errorf("OpenList 获取文件失败(code=%d): %s", res.Code, res.Message)
	}
	if res.Data.RawURL == "" {
		return "", fmt.Errorf("OpenList 未返回 raw_url")
	}
	return res.Data.RawURL, nil
}

func (c *Client) TestConnection() error {
	return c.TestConnectionContext(context.Background())
}

func (c *Client) TestConnectionContext(ctx context.Context) error {
	testClient := *c
	testClient.AccountID = 0
	testClient.CacheTTL = 0
	testClient.CustomCachePolicies = ""
	if testClient.AuthMode == "password" {
		token, err := testClient.loginContext(ctx)
		if err != nil {
			return err
		}
		testClient.AuthMode = "token"
		testClient.StaticToken = token
		testClient.Username = ""
		testClient.Password = ""
	}
	_, err := testClient.ListDirectoryContext(ctx, "/", false)
	return err
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
	secret := c.Password
	if c.AuthMode == "token" {
		secret = c.StaticToken
	}
	h := sha256.New()
	for _, part := range []string{
		fmt.Sprintf("%d", c.AccountID),
		c.BaseURL,
		"",
		c.AuthMode,
		c.Username,
		secret,
	} {
		_, _ = fmt.Fprintf(h, "%d:", len(part))
		_, _ = h.Write([]byte(part))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func (c *Client) tokenCacheKey(fingerprint string) string {
	return fmt.Sprintf("openlist:%d:%s", c.AccountID, fingerprint)
}

func (c *Client) directoryCacheKey(fingerprint, pathStr string) string {
	h := sha256.New()
	for _, part := range []string{
		fingerprint,
		fmt.Sprintf("%d", c.CacheTTL),
		c.CustomCachePolicies,
		pathStr,
	} {
		_, _ = fmt.Fprintf(h, "%d:", len(part))
		_, _ = h.Write([]byte(part))
	}
	return fmt.Sprintf("openlist:%d:dir:%x", c.AccountID, h.Sum(nil))
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
	prefix := fmt.Sprintf("openlist:%d:", accountID)
	cacheMutex.Lock()
	for key := range globalTokenCache {
		if strings.HasPrefix(key, prefix) {
			delete(globalTokenCache, key)
		}
	}
	delete(tokenRefreshLocks, accountID)
	cacheMutex.Unlock()

	dirCacheMutex.Lock()
	for key := range dirCache {
		if strings.HasPrefix(key, prefix) {
			delete(dirCache, key)
		}
	}
	dirCacheMutex.Unlock()
}
