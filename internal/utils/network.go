package utils

import (
	"fmt"
	"github.com/rs/zerolog/log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	MaxRedirectAttempts = 10
	RedirectTimeout     = 10 * time.Second
)

var (
	ErrInvalidLocationHeader = fmt.Errorf("重定向 Location 头无效")
	ErrMaxRedirectsExceeded  = fmt.Errorf("超过最大重定向次数限制（%d）", MaxRedirectAttempts)
)

func GetFinalURL(rawURL string, ua string) (string, error) {
	startTime := time.Now()
	defer func() {
		log.Debug().Str("url", rawURL).Str("duration", time.Since(startTime).String()).Msg("获取最终URL耗时")
	}()

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("非法 URL： %w", err)
	}
	if parsedURL.Scheme == "" {
		return "", fmt.Errorf("URL 缺少协议头： %s", parsedURL)
	}

	client := &http.Client{
		Timeout: RedirectTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	currentURL := parsedURL.String()
	visited := make(map[string]struct{}, MaxRedirectAttempts)
	redirectChain := make([]string, 0, MaxRedirectAttempts+1)

	for i := 0; i <= MaxRedirectAttempts; i++ {
		if _, exists := visited[currentURL]; exists {
			return "", fmt.Errorf("检测到循环重定向，重定向链: %s", strings.Join(redirectChain, " -> "))
		}
		visited[currentURL] = struct{}{}
		redirectChain = append(redirectChain, currentURL)

		req, err := http.NewRequest(http.MethodHead, currentURL, nil)
		if err != nil {
			return "", fmt.Errorf("创建请求失败: %w", err)
		}
		if ua != "" {
			req.Header.Set("User-Agent", ua)
		}

		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("发送 HTTP 请求失败：%w", err)
		}
		resp.Body.Close()

		if resp.StatusCode >= http.StatusMultipleChoices && resp.StatusCode < http.StatusBadRequest {
			location, err := resp.Location()
			if err != nil {
				return "", ErrInvalidLocationHeader
			}
			currentURL = location.String()
			if !strings.HasPrefix(currentURL, "http") {
				fullURL := fmt.Sprintf("%s://%s%s", req.URL.Scheme, req.URL.Host, location)
				log.Debug().Str("old", currentURL).Str("new", fullURL).Msg("拼接完整 URL")
				currentURL = fullURL
			}
			continue
		}
		if len(redirectChain) > 1 {
			log.Debug().Msgf("重定向链：%s", strings.Join(redirectChain, " -> "))
		}
		return currentURL, nil
	}
	return "", ErrMaxRedirectsExceeded
}