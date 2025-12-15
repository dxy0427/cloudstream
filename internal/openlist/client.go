package openlist

import (
	"bytes"
	"cloudstream/internal/models"
	"encoding/json"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog/log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// 全局 Token 缓存结构
type tokenCacheItem struct {
	Token     string
	ExpiresAt time.Time
}

var (
	// 全局缓存 map: AccountID -> TokenInfo
	globalTokenCache = make(map[uint]*tokenCacheItem)
	cacheMutex       sync.RWMutex
)

type Client struct {
	AccountID   uint
	BaseURL     string
	StaticToken string
	Username    string
	Password    string
	CacheTTL    int // 目录缓存时间(分钟)
	HTTPClient  *http.Client
}

func NewClient(account models.Account) *Client {
	base := strings.TrimSpace(account.OpenListURL)
	if base != "" && !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	base = strings.TrimRight(base, "/")

	return &Client{
		AccountID:   account.ID,
		BaseURL:     base,
		StaticToken: strings.TrimSpace(account.OpenListToken),
		Username:    strings.TrimSpace(account.OpenListUsername),
		Password:    strings.TrimSpace(account.OpenListPassword),
		CacheTTL:    account.CacheTTL,
		HTTPClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

// 获取有效 Token (带自动登录、JWT解析和缓存)
func (c *Client) getToken() (string, error) {
	// 1. 优先使用手动填写的静态 Token
	if c.StaticToken != "" {
		return c.StaticToken, nil
	}

	if c.Username == "" || c.Password == "" {
		return "", fmt.Errorf("未配置 Token 且未配置用户名/密码")
	}

	// 2. 检查全局缓存
	cacheMutex.RLock()
	if item, exists := globalTokenCache[c.AccountID]; exists {
		// 提前 5 分钟认为过期，触发刷新
		if time.Now().Before(item.ExpiresAt.Add(-5 * time.Minute)) {
			token := item.Token
			cacheMutex.RUnlock()
			return token, nil
		}
	}
	cacheMutex.RUnlock()

	// 3. 缓存未命中或已过期，执行登录
	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	// 双重检查防止并发穿透
	if item, exists := globalTokenCache[c.AccountID]; exists {
		if time.Now().Before(item.ExpiresAt.Add(-5 * time.Minute)) {
			return item.Token, nil
		}
	}

	// 执行登录
	token, err := c.login()
	if err != nil {
		return "", err
	}

	// --- 核心优化：解析 JWT 获取真实过期时间 ---
	expiration := time.Now().Add(24 * time.Hour) // 默认兜底：24小时

	// 使用 ParseUnverified 解析，因为我们要读取 payload，且信任服务端返回的 Token
	parsedToken, _, err := jwt.NewParser().ParseUnverified(token, jwt.MapClaims{})
	if err == nil {
		if claims, ok := parsedToken.Claims.(jwt.MapClaims); ok {
			// 获取 exp 字段 (Unix 时间戳)
			if exp, ok := claims["exp"].(float64); ok {
				realExp := time.Unix(int64(exp), 0)
				// 只有当解析出的时间是未来的时间才采纳
				if realExp.After(time.Now()) {
					expiration = realExp
					log.Info().Uint("accountID", c.AccountID).Time("expireAt", expiration).Msg("OpenList Token 有效期已自动同步")
				}
			}
		}
	} else {
		log.Warn().Err(err).Msg("解析 OpenList JWT 失败，将使用默认过期时间")
	}
	// ------------------------------------------

	// 更新缓存
	globalTokenCache[c.AccountID] = &tokenCacheItem{
		Token:     token,
		ExpiresAt: expiration,
	}

	return token, nil
}

// 调用 Alist 登录接口
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

	// 自动获取 Token
	token, err := c.getToken()
	if err != nil {
		return err
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("编码 OpenList 请求失败: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.BaseURL+apiPath, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("创建 OpenList 请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("请求 OpenList 失败: %w", err)
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("解析 OpenList 响应失败: %w", err)
	}

	return nil
}

func (c *Client) ListDirectory(pathStr string, refresh bool) ([]FileInfo, error) {
	if pathStr == "" {
		pathStr = "/"
	}
	if !strings.HasPrefix(pathStr, "/") {
		pathStr = "/" + pathStr
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
	// 强制清理一次缓存以测试真实连接
	cacheMutex.Lock()
	delete(globalTokenCache, c.AccountID)
	cacheMutex.Unlock()
	
	_, err := c.ListDirectory("/", false)
	return err
}