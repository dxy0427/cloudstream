package pan123

import (
	"bytes"
	"cloudstream/internal/models"
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog/log"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// 123网盘API固定配置
const (
	ApiBaseURL = "https://open-api.123pan.com" // API基础地址
	Timeout    = 20 * time.Second              // HTTP请求超时时间
	UserAgent  = "CloudStream/1.0.0"           // 请求User-Agent标识
)

// 全局AccessToken缓存：按账户ID存储，带读写锁防止并发冲突，记录过期时间
var (
	tokenCaches = make(map[uint]*struct {
		sync.RWMutex
		Token     string    // 缓存的AccessToken
		ExpiresAt time.Time // Token过期时间
	})
	mapMutex sync.Mutex // tokenCaches的互斥锁（确保并发安全）
)

// Client：123网盘客户端实例（关联账户信息和HTTP客户端）
type Client struct {
	HTTPClient *http.Client   // HTTP客户端（带超时配置）
	Account    models.Account // 关联的云账户配置（ClientID、Secret等）
}

// NewClient：创建123网盘客户端实例
func NewClient(account models.Account) *Client {
	return &Client{
		HTTPClient: &http.Client{Timeout: Timeout}, // 初始化带超时的HTTP客户端
		Account:    account,                        // 绑定云账户信息
	}
}

// getAccessToken：获取并缓存AccessToken（提前5分钟刷新，双重检查锁确保并发安全）
func (c *Client) getAccessToken() (string, error) {
	// 初始化当前账户的缓存（若不存在）
	mapMutex.Lock()
	if _, ok := tokenCaches[c.Account.ID]; !ok {
		tokenCaches[c.Account.ID] = &struct {
			sync.RWMutex
			Token     string
			ExpiresAt time.Time
		}{}
	}
	cache := tokenCaches[c.Account.ID]
	mapMutex.Unlock()

	// 读锁检查：Token未过期（提前5分钟刷新）则直接返回
	cache.RLock()
	if cache.Token != "" && time.Now().Before(cache.ExpiresAt.Add(-5*time.Minute)) {
		token := cache.Token
		cache.RUnlock()
		return token, nil
	}
	cache.RUnlock()

	// 写锁更新：再次检查避免并发更新，然后请求新Token
	cache.Lock()
	defer cache.Unlock()
	if cache.Token != "" && time.Now().Before(cache.ExpiresAt.Add(-5*time.Minute)) {
		return cache.Token, nil
	}

	// 构造AccessToken请求（POST JSON参数）
	apiURL := ApiBaseURL + "/api/v1/access_token"
	bodyData, _ := json.Marshal(map[string]string{
		"client_id":     c.Account.ClientID,
		"client_secret": c.Account.ClientSecret,
	})

	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewBuffer(bodyData))
	if err != nil {
		return "", fmt.Errorf("创建 AccessToken 请求失败: %w", err)
	}

	// 设置请求头（符合123网盘API要求）
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("platform", "open_platform")
	req.Header.Set("User-Agent", UserAgent)

	// 发送请求并解析响应
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求 AccessToken 失败: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp AccessTokenResp
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("解析 AccessToken 响应失败: %w", err)
	}

	// 校验API返回状态
	if tokenResp.Code != 0 {
		return "", fmt.Errorf("获取 AccessToken API 错误 (code: %d): %s", tokenResp.Code, tokenResp.Message)
	}
	if tokenResp.Data.AccessToken == "" {
		return "", fmt.Errorf("API 未返回有效的 AccessToken")
	}

	// 处理两种过期时间格式（RFC3339 或 YYYY-MM-DD HH:MM:SS）
	expires, err := time.Parse(time.RFC3339, tokenResp.Data.ExpiredAt)
	if err != nil {
		expires, err = time.Parse("2006-01-02 15:04:05", tokenResp.Data.ExpiredAt)
		if err != nil {
			return "", fmt.Errorf("解析 Token 过期时间失败: %w", err)
		}
	}

	// 更新缓存并返回
	cache.Token = tokenResp.Data.AccessToken
	cache.ExpiresAt = expires
	log.Info().Str("account", c.Account.Name).Msg("AccessToken 已成功刷新并缓存")

	return cache.Token, nil
}

// sendAuthorizedRequest：发送带Bearer Token的授权请求，统一解析API响应格式
func (c *Client) sendAuthorizedRequest(method, endpoint, accessToken string, queryParams map[string]interface{}) (json.RawMessage, error) {
	// 拼接完整请求URL（基础地址+接口路径+查询参数）
	fullURL, _ := url.Parse(ApiBaseURL)
	fullURL.Path = endpoint

	q := fullURL.Query()
	if queryParams != nil {
		for k, v := range queryParams {
			q.Set(k, fmt.Sprintf("%v", v)) // 转换参数为字符串格式
		}
	}
	fullURL.RawQuery = q.Encode()

	// 创建HTTP请求
	req, err := http.NewRequest(method, fullURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("创建授权请求失败: %w", err)
	}

	// 设置授权头和其他必要头信息
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("platform", "open_platform")
	req.Header.Set("User-Agent", UserAgent)

	// 发送请求并解析响应
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送授权请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 统一解析API响应（BaseResp+Data字段）
	var result struct {
		BaseResp
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析授权响应 JSON 失败: %w", err)
	}

	// 校验API返回状态
	if result.Code != 0 {
		return nil, fmt.Errorf("123Pan API 错误 (code: %d): %s", result.Code, result.Message)
	}

	return result.Data, nil
}

// ListFiles：分页拉取云盘文件列表（按父目录ID，只返回未删除文件）
func (c *Client) ListFiles(parentFileId int64, limit int, lastFileId int64) ([]FileInfo, int64, error) {
	// 先获取有效的AccessToken
	accessToken, err := c.getAccessToken()
	if err != nil {
		return nil, 0, fmt.Errorf("获取文件列表前，获取 AccessToken 失败: %w", err)
	}

	// 构造请求参数（trashed=0：过滤已删除文件）
	params := map[string]interface{}{
		"parentFileId": parentFileId,
		"limit":        limit,
		"trashed":      0,
	}
	if lastFileId > 0 {
		params["lastFileId"] = lastFileId // 分页续传：传入上一页最后一个文件ID
	}

	// 发送授权请求，解析文件列表数据
	rawData, err := c.sendAuthorizedRequest(http.MethodGet, "/api/v2/file/list", accessToken, params)
	if err != nil {
		return nil, 0, err
	}

	var listData struct {
		FileList   []FileInfo `json:"fileList"` // 当前页文件列表
		LastFileId int64      `json:"lastFileId"`// 下一页起始的文件ID（-1表示无更多）
	}
	if err := json.Unmarshal(rawData, &listData); err != nil {
		return nil, 0, fmt.Errorf("解析文件列表数据失败: %w", err)
	}

	return listData.FileList, listData.LastFileId, nil
}

// GetDownloadURL：获取文件的云盘下载链接
func (c *Client) GetDownloadURL(fileID int64) (string, error) {
	// 先获取有效的AccessToken
	accessToken, err := c.getAccessToken()
	if err != nil {
		return "", fmt.Errorf("获取下载链接前，获取 AccessToken 失败: %w", err)
	}

	// 构造请求参数（文件ID转字符串）
	params := map[string]interface{}{
		"fileId": strconv.FormatInt(fileID, 10),
	}

	// 发送请求，解析下载链接
	rawData, err := c.sendAuthorizedRequest(http.MethodGet, "/api/v1/file/download_info", accessToken, params)
	if err != nil {
		return "", err
	}

	var downloadInfo struct {
		DownloadURL string `json:"downloadUrl"` // 云盘直接下载链接
	}
	if err := json.Unmarshal(rawData, &downloadInfo); err != nil {
		return "", fmt.Errorf("解析下载链接数据失败: %w", err)
	}
	if downloadInfo.DownloadURL == "" {
		return "", fmt.Errorf("API 未返回有效的下载链接")
	}

	return downloadInfo.DownloadURL, nil
}

// GetStreamURL：生成文件的播放链接（基于账户配置的基础URL，默认本地服务）
func (c *Client) GetStreamURL(accountID uint, fileID string) (string, error) {
	// 处理基础URL：去除末尾斜杠（避免拼接时出现//）
	baseURL := c.Account.StrmBaseURL
	if len(baseURL) > 0 && baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}

	// 基础URL为空时，默认使用本地服务地址（127.0.0.1:12398）
	if baseURL == "" {
		baseURL = "http://127.0.0.1:12398"
	}

	// 校验文件ID格式（必须是有效数字）
	if _, err := strconv.ParseInt(fileID, 10, 64); err != nil {
		return "", fmt.Errorf("无效的文件 ID: %s", fileID)
	}

	// 拼接播放链接：baseURL/api/v1/stream/账户ID/文件ID
	return fmt.Sprintf("%s/api/v1/stream/%d/%s", baseURL, accountID, fileID), nil
}

// GetAccessTokenForTest：测试用获取AccessToken（不缓存，直接请求，用于账户连接校验）
func (c *Client) GetAccessTokenForTest() (string, error) {
	// 构造请求（逻辑同getAccessToken，但不写入缓存）
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

	// 校验API返回（同getAccessToken，但不处理缓存）
	if tokenResp.Code != 0 {
		return "", fmt.Errorf("API 错误 (code: %d): %s", tokenResp.Code, tokenResp.Message)
	}
	if tokenResp.Data.AccessToken == "" {
		return "", fmt.Errorf("API 未返回有效的 AccessToken")
	}

	// 仅校验过期时间格式，不缓存
	_, err = time.Parse(time.RFC3339, tokenResp.Data.ExpiredAt)
	if err != nil {
		_, err = time.Parse("2006-01-02 15:04:05", tokenResp.Data.ExpiredAt)
		if err != nil {
			return "", fmt.Errorf("解析 Token 过期时间失败: %w", err)
		}
	}

	return tokenResp.Data.AccessToken, nil
}