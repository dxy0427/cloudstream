package mediaserver

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

type JellyfinClient struct {
	client *resty.Client
	host   string
	apiKey string
}

func NewJellyfinClient(host, apiKey string) *JellyfinClient {
	return &JellyfinClient{
		client: resty.New().SetTimeout(5 * time.Second),
		host:   strings.TrimRight(host, "/"),
		apiKey: apiKey,
	}
}

func (j *JellyfinClient) Ping() error {
	url := fmt.Sprintf("%s/System/Info", j.host)
	req := j.authenticatedRequest()
	resp, err := req.Get(url)
	if err != nil {
		return err
	}
	if resp.StatusCode() != 200 {
		return fmt.Errorf("jellyfin 连接失败，状态码: %d", resp.StatusCode())
	}
	return nil
}

func (j *JellyfinClient) GetItemInfo(itemId string, mediaSourceId string) (string, error) {
	url := fmt.Sprintf("%s/Items", j.host)
	req := j.client.R().
		SetQueryParam("Ids", itemId).
		SetQueryParam("Fields", "Path,MediaSources").
		SetQueryParam("Limit", "1")

	if j.apiKey != "" {
		req.SetHeader("X-Emby-Token", j.apiKey)
	}

	resp, err := req.Get(url)
	if err != nil {
		return "", err
	}
	if resp.StatusCode() != 200 {
		return "", fmt.Errorf("jellyfin api error: %d", resp.StatusCode())
	}

	var res commonItemsResponse
	if err := json.Unmarshal(resp.Body(), &res); err != nil {
		return "", err
	}

	return commonGetItemPath(res, mediaSourceId)
}

func (j *JellyfinClient) authenticatedRequest() *resty.Request {
	req := j.client.R()
	if j.apiKey != "" {
		req.SetHeader("X-Emby-Token", j.apiKey)
	}
	return req
}

func (j *JellyfinClient) Close() {
	j.client.GetClient().CloseIdleConnections()
}
