package openlist

import (
	"bytes"
	"cloudstream/internal/models"
	"encoding/json"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog/log"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

type tokenCacheItem struct {
	Token     string
	ExpiresAt time.Time
}

var (
	globalTokenCache = make(map[uint]*tokenCacheItem)
	cacheMutex       sync.RWMutex
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

type Client struct {
	AccountID          uint
	BaseURL            string
	StaticToken        string
	Username           string
	Password           string
	CacheTTL           int
	CustomCachePolicies string
	HTTPClient         *http.Client
}

func NewClient(account models.Account) *Client {
	base := strings.TrimSpace(account.OpenListURL)
	if base != "" && !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	base = strings.TrimRight(base, "/")

	return &Client{
		AccountID:          account.ID,
		BaseURL:            base,
		StaticToken:        strings.TrimSpace(account.OpenListToken),
		Username:           strings.TrimSpace(account.OpenListUsername),
		Password:           strings.TrimSpace(account.OpenListPassword),
		CacheTTL:           account.CacheTTL,
		CustomCachePolicies: account.CustomCachePolicies,
		HTTPClient:         &http.Client{Timeout: 30 * time.Second},
	}
}

// 获取有效 Token (带自动登录、JWT解析和缓存)
func (c *Client) getToken() (string, error) {
	if c.StaticToken != "" {
		return c.StaticToken, nil
	}

	if c.Username == "" || c.Password == "" {
		return "", fmt.Errorf("未配置 Token 且未配置用户名/密码")
	}

	cacheMutex.RLock()
	if item, exists := globalTokenCache[c.AccountID]; exists {
		if time.Now().Before(item.ExpiresAt.Add(-5 * time.Minute)) {
			token := item.Token
			cacheMutex.RUnlock()
			return token, nil
		}
	}
	cacheMutex.RUnlock()

	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	if item, exists := globalTokenCache[c.AccountID]; exists {
		if time.Now().Before(item.ExpiresAt.Add(-5 * time.Minute)) {
			return item.Token, nil
		}
	}

	token, err := c.login()
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

	globalTokenCache[c.AccountID] = &tokenCacheItem{
		Token:     token,
		ExpiresAt: expiration,
	}

	return token, nil
}

func (c *Client) invalidateCache() {
	cacheMutex.Lock()
	delete(globalTokenCache, c.AccountID)
	cacheMutex.Unlock()
	log.Warn().Uint("accountID", c.AccountID).Msg("OpenList Token 已被标记为失效，下次请求将重新登录")
}

func (c *Client) login() (string, error) {
	apiPath := "/api/auth/login"
	body := map[string]string{
		"username": c.Username,
		"password": c.Password,
	}
	
	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, c.BaseURL+apiPath, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("登录请求失败: %w", err)
	}
	defer resp.Body.Close()

	var res struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Token string `json:"token"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", fmt.Errorf("解析登录响应失败: %w", err)
	}

	if res.Code != 200 {
		return "", fmt.Errorf("登录失败(code=%d): %s", res.Code, res.Message)
	}

	return res.Data.Token, nil
}

func (c *Client) doPostJSON(apiPath string, body any, out any) error {
	if c.BaseURL == "" {
		return fmt.Errorf("OpenList 地址未配置")
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("编码请求失败: %w", err)
	}

	maxAuthRetries := 2
	for authAttempt := 0; authAttempt < maxAuthRetries; authAttempt++ {

		token, err := c.getToken()
		if err != nil {
			return err
		}

		maxNetRetries := 3
		var lastNetErr error
		var resp *http.Response
		var respBody []byte

		for i := 0; i < maxNetRetries; i++ {
			if i > 0 {
				time.Sleep(time.Duration(1<<uint(i-1)) * time.Second)
			}

			req, err := http.NewRequest(http.MethodPost, c.BaseURL+apiPath, bytes.NewReader(jsonData))
			if err != nil {
				return err
			}

			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", token)
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

			resp, err = c.HTTPClient.Do(req)
			if err != nil {
				lastNetErr = err
				continue 
			}

			respBody, err = io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
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
			if authAttempt == 0 && c.StaticToken == "" {
				c.invalidateCache()
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
				if authAttempt == 0 && c.StaticToken == "" {
					c.invalidateCache()
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

// getCacheTTL 根据自定义缓存策略返回指定路径的 TTL（分钟）
func getCacheTTL(policies string, defaultTTL int, dirPath string) int {
	if policies == "" {
		return defaultTTL
	}
	for _, line := range strings.Split(policies, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pattern, ttlStr, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if matched, _ := path.Match(pattern, dirPath); matched {
			if ttl, err := strconv.Atoi(strings.TrimSpace(ttlStr)); err == nil {
				return ttl
			}
		}
	}
	return defaultTTL
}

func (c *Client) ListDirectory(pathStr string, refresh bool) ([]FileInfo, error) {
	if pathStr == "" {
		pathStr = "/"
	}
	if !strings.HasPrefix(pathStr, "/") {
		pathStr = "/" + pathStr
	}

	cacheTTL := getCacheTTL(c.CustomCachePolicies, c.CacheTTL, pathStr)
	cacheKey := fmt.Sprintf("openlist:%d:%s", c.AccountID, pathStr)

	if cacheTTL > 0 && !refresh {
		dirCacheMutex.RLock()
		if item, ok := dirCache[cacheKey]; ok {
			if time.Now().Before(item.ExpiresAt) {
				dirCacheMutex.RUnlock()
				return item.Data, nil
			}
		}
		dirCacheMutex.RUnlock()
	}

	body := map[string]any{
		"path":     pathStr,
		"password": "",
		"page":     1,
		"per_page": 0,
		"refresh":  refresh,
	}

	var res listResponse
	if err := c.doPostJSON("/api/fs/list", body, &res); err != nil {
		return nil, err
	}
	if res.Code != 200 {
		return nil, fmt.Errorf("OpenList 列表失败(code=%d): %s", res.Code, res.Message)
	}

	if cacheTTL > 0 {
		dirCacheMutex.Lock()
		dirCache[cacheKey] = &dirCacheEntry{
			Data:      res.Data.Content,
			ExpiresAt: time.Now().Add(time.Duration(cacheTTL) * time.Minute),
		}
		dirCacheMutex.Unlock()
	}

	return res.Data.Content, nil
}

func (c *Client) GetRawURL(pathStr string) (string, error) {
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
	if err := c.doPostJSON("/api/fs/get", body, &res); err != nil {
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
	c.invalidateCache()
	_, err := c.ListDirectory("/", false)
	return err
}