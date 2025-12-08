package openlist

import (
	"bytes"
	"cloudstream/internal/models"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

func NewClient(account models.Account) *Client {
	base := strings.TrimSpace(account.OpenListURL)
	if base != "" && !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	base = strings.TrimRight(base, "/")

	return &Client{
		BaseURL:    base,
		Token:      strings.TrimSpace(account.OpenListToken),
		HTTPClient: &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *Client) doPostJSON(apiPath string, body any, out any) error {
	if c.BaseURL == "" {
		return fmt.Errorf("OpenList 地址未配置")
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
	if c.Token != "" {
		req.Header.Set("Authorization", c.Token)
	}

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
	_, err := c.ListDirectory("/", false)
	return err
}