package mediaserver

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

type EmbyClient struct {
	client *resty.Client
	host   string
	apiKey string
}

func NewEmbyClient(host, apiKey string) *EmbyClient {
	return &EmbyClient{
		client: resty.New().SetTimeout(5 * time.Second),
		host:   strings.TrimRight(host, "/"),
		apiKey: apiKey,
	}
}

func (e *EmbyClient) Ping() error {
	url := fmt.Sprintf("%s/System/Info", e.host)
	req := e.authenticatedRequest()
	resp, err := req.Get(url)
	if err != nil {
		return err
	}
	if resp.StatusCode() != 200 {
		return fmt.Errorf("emby 连接失败，状态码: %d", resp.StatusCode())
	}
	return nil
}

func (e *EmbyClient) GetItemInfo(itemId string, mediaSourceId string, accessToken string) (string, error) {
	url := fmt.Sprintf("%s/Items", e.host)
	req := e.client.R().
		SetQueryParam("Ids", itemId).
		SetQueryParam("Fields", "Path,MediaSources").
		SetQueryParam("Limit", "1")
	if accessToken != "" {
		req.SetHeader("X-Emby-Token", accessToken)
	}

	resp, err := req.Get(url)
	if err != nil {
		return "", err
	}
	if resp.StatusCode() != 200 {
		return "", fmt.Errorf("emby api error: %d", resp.StatusCode())
	}

	var res commonItemsResponse
	if err := json.Unmarshal(resp.Body(), &res); err != nil {
		return "", err
	}

	return commonGetItemPath(res, mediaSourceId)
}

func (e *EmbyClient) authenticatedRequest() *resty.Request {
	req := e.client.R()
	if e.apiKey != "" {
		req.SetHeader("X-Emby-Token", e.apiKey)
	}
	return req
}

func (e *EmbyClient) Close() {
	e.client.GetClient().CloseIdleConnections()
}
