package pan123

import (
	"bytes"
	"cloudstream/internal/models"
	"cloudstream/internal/openlist"
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog/log"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const (
	ApiBaseURL = "https://open-api.123pan.com"
	Timeout    = 60 * time.Second
	UserAgent  = "CloudStream/1.0.0"
)

// --- 缓存结构 ---

// 123Pan AccessToken 缓存 (这个必须有)
type tokenCacheItem struct {
	sync.RWMutex
	Token     string
	ExpiresAt time.Time
}

// 目录列表缓存 (TTL 可配置)
type listCacheItem struct {
	Data       []FileInfo
	NextFileId int64
	ExpiresAt  time.Time
}

var (
	tokenCaches = make(map[uint]*tokenCacheItem)
	mapMutex    sync.Mutex

	listCache      = make(map[string]*listCacheItem)
	listCacheMutex sync.RWMutex
)

// 内存保护：仅清理列表缓存
func cleanupCache() {
	listCacheMutex.Lock()
	if len(listCache) > 3000 {
		log.Info().Int("count", len(listCache)).Msg("触发列表缓存清理")
		for k, v := range listCache {
			if time.Now().After(v.ExpiresAt) {
				delete(listCache, k)
			}
		}
		// 如果清理后还是很满，强制重置
		if len(listCache) > 3000 {
			listCache = make(map[string]*listCacheItem)
		}
	}
	listCacheMutex.Unlock()
}

type Client struct {
	HTTPClient     *http.Client
	Account        models.Account
	OpenListClient *openlist.Client
}

func NewClient(account models.Account) *Client {
	client := &Client{
		HTTPClient: &http.Client{Timeout: Timeout},
		Account:    account,
	}
	if account.Type == models.AccountTypeOpenList {
		client.OpenListClient = openlist.NewClient(account)
	}
	
	// 触发一次非阻塞清理
	if len(listCache) > 3000 {
		go cleanupCache()
	}
	
	return client
}

// 获取 123Pan AccessToken (带缓存)
func (c *Client) getAccessToken() (string, error) {
	mapMutex.Lock()
	if _, ok := tokenCaches[c.Account.ID]; !ok {
		tokenCaches[c.Account.ID] = &tokenCacheItem{}
	}
	cache := tokenCaches[c.Account.ID]
	mapMutex.Unlock()

	cache.RLock()
	if cache.Token != "" && time.Now().Before(cache.ExpiresAt.Add(-5*time.Minute)) {
		token := cache.Token
		cache.RUnlock()
		return token, nil
	}
	cache.RUnlock()

	cache.Lock()
	defer cache.Unlock()

	if cache.Token != "" && time.Now().Before(cache.ExpiresAt.Add(-5*time.Minute)) {
		return cache.Token, nil
	}

	apiURL := ApiBaseURL + "/api/v1/access_token"
	bodyData, _ := json.Marshal(map[string]string{
		"client_id":     c.Account.ClientID,
		"client_secret": c.Account.ClientSecret,
	})

	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewBuffer(bodyData))
	if err != nil {
		return "", fmt.Errorf("创建 AccessToken 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("platform", "open_platform")
	req.Header.Set("User-Agent", UserAgent)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求 AccessToken 失败: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp AccessTokenResp
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("解析 AccessToken 响应失败: %w", err)
	}
	if tokenResp.Code != 0 {
		return "", fmt.Errorf("获取 AccessToken API 错误 (code: %d): %s", tokenResp.Code, tokenResp.Message)
	}
	if tokenResp.Data.AccessToken == "" {
		return "", fmt.Errorf("API 未返回有效的 AccessToken")
	}

	expires, err := time.Parse(time.RFC3339, tokenResp.Data.ExpiredAt)
	if err != nil {
		expires, err = time.Parse("2006-01-02 15:04:05", tokenResp.Data.ExpiredAt)
		if err != nil {
			return "", fmt.Errorf("解析 Token 过期时间失败: %w", err)
		}
	}

	cache.Token = tokenResp.Data.AccessToken
	cache.ExpiresAt = expires
	log.Info().Str("account", c.Account.Name).Msg("AccessToken 已成功刷新")

	return cache.Token, nil
}

func ClearTokenCache(accountID uint) {
	mapMutex.Lock()
	defer mapMutex.Unlock()
	if _, ok := tokenCaches[accountID]; ok {
		delete(tokenCaches, accountID)
		log.Info().Uint("accountID", accountID).Msg("已清理账户的 AccessToken 缓存")
	}
}

func (c *Client) sendAuthorizedRequest(method, endpoint, accessToken string, queryParams map[string]interface{}) (json.RawMessage, error) {
	fullURL, _ := url.Parse(ApiBaseURL)
	fullURL.Path = endpoint
	q := fullURL.Query()
	if queryParams != nil {
		for k, v := range queryParams {
			q.Set(k, fmt.Sprintf("%v", v))
		}
	}
	fullURL.RawQuery = q.Encode()

	req, err := http.NewRequest(method, fullURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("创建授权请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("platform", "open_platform")
	req.Header.Set("User-Agent", UserAgent)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送授权请求失败: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		BaseResp
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析授权响应 JSON 失败: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("123Pan API 错误 (code: %d): %s", result.Code, result.Message)
	}
	return result.Data, nil
}

// ListFiles 获取文件列表 (根据 Account.CacheTTL 决定是否缓存)
func (c *Client) ListFiles(parentFileId int64, limit int, lastFileId int64, parentPath string) ([]FileInfo, int64, error) {
	if c.Account.Type == models.AccountType123Pan {
		cacheKey := fmt.Sprintf("list:%d:%d:%d", c.Account.ID, parentFileId, lastFileId)
		
		// 1. 如果配置了 TTL，检查缓存
		if c.Account.CacheTTL > 0 {
			listCacheMutex.RLock()
			if item, ok := listCache[cacheKey]; ok {
				if time.Now().Before(item.ExpiresAt) {
					listCacheMutex.RUnlock()
					return item.Data, item.NextFileId, nil
				}
			}
			listCacheMutex.RUnlock()
		}

		// 2. 调用 API
		accessToken, err := c.getAccessToken()
		if err != nil {
			return nil, 0, fmt.Errorf("获取 AccessToken 失败: %w", err)
		}
		params := map[string]interface{}{
			"parentFileId": parentFileId,
			"limit":        limit,
			"trashed":      0,
			"orderBy":      "fileId",
			"orderDirection": "asc",
		}
		if lastFileId > 0 {
			params["lastFileId"] = lastFileId
		}
		rawData, err := c.sendAuthorizedRequest(http.MethodGet, "/api/v2/file/list", accessToken, params)
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

		// 3. 如果配置了 TTL，写入缓存
		if c.Account.CacheTTL > 0 {
			listCacheMutex.Lock()
			listCache[cacheKey] = &listCacheItem{
				Data:       listData.FileList,
				NextFileId: listData.LastFileId,
				ExpiresAt:  time.Now().Add(time.Duration(c.Account.CacheTTL) * time.Minute),
			}
			listCacheMutex.Unlock()
		}

		return listData.FileList, listData.LastFileId, nil
	}
	return []FileInfo{}, -1, nil
}

// ListOpenListDirectory 获取 OpenList 列表 (根据 Account.CacheTTL 决定是否缓存)
func (c *Client) ListOpenListDirectory(parentPath string) ([]FileInfo, error) {
	if c.OpenListClient == nil {
		return nil, fmt.Errorf("OpenList 客户端未初始化")
	}

	cacheKey := fmt.Sprintf("list:%d:%s", c.Account.ID, parentPath)

	// 1. 如果配置了 TTL，检查缓存
	if c.Account.CacheTTL > 0 {
		listCacheMutex.RLock()
		if item, ok := listCache[cacheKey]; ok {
			if time.Now().Before(item.ExpiresAt) {
				listCacheMutex.RUnlock()
				return item.Data, nil
			}
		}
		listCacheMutex.RUnlock()
	}

	// 2. 调用 API
	openListFiles, err := c.OpenListClient.ListDirectory(parentPath, false)
	if err != nil {
		return nil, fmt.Errorf("OpenList 列表失败: %w", err)
	}

	var files []FileInfo
	for _, item := range openListFiles {
		fileType := 0
		if item.IsDir {
			fileType = 1
		}
		files = append(files, FileInfo{
			FileId:   0,
			FileName: item.Name,
			FileType: fileType,
			Size:     item.Size,
			Trashed:  0,
		})
	}

	// 3. 如果配置了 TTL，写入缓存
	if c.Account.CacheTTL > 0 {
		listCacheMutex.Lock()
		listCache[cacheKey] = &listCacheItem{
			Data:      files,
			ExpiresAt: time.Now().Add(time.Duration(c.Account.CacheTTL) * time.Minute),
		}
		listCacheMutex.Unlock()
	}

	return files, nil
}

// GetDownloadURL 获取直链 (无缓存，直接穿透)
func (c *Client) GetDownloadURL(identifier interface{}) (string, error) {
	var finalURL string
	var err error

	if c.Account.Type == models.AccountTypeOpenList {
		if c.OpenListClient == nil {
			return "", fmt.Errorf("OpenList 客户端未初始化")
		}
		pathStr, ok := identifier.(string)
		if !ok {
			return "", fmt.Errorf("OpenList 需要路径参数")
		}
		finalURL, err = c.OpenListClient.GetRawURL(pathStr)
	} else {
		// 123 Pan
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

		accessToken, err := c.getAccessToken()
		if err != nil {
			return "", fmt.Errorf("获取 AccessToken 失败: %w", err)
		}
		params := map[string]interface{}{"fileId": strconv.FormatInt(fileID, 10)}
		rawData, err := c.sendAuthorizedRequest(http.MethodGet, "/api/v1/file/download_info", accessToken, params)
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
		finalURL = downloadInfo.DownloadURL
	}

	return finalURL, err
}

func (c *Client) GetAccessTokenForTest() (string, error) {
	return c.getAccessToken()
}