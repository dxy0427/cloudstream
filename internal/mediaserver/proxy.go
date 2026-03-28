package mediaserver

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ProxyServer 媒体服务器代理
type ProxyServer struct {
	cfg         *Config
	mediaServer MediaServerClient
	httpClient  *http.Client
	cache       *Cache
}

// Cache 内存缓存
type Cache struct {
	data map[string]cacheEntry
	mu   sync.RWMutex
}

type cacheEntry struct {
	value     string
	expiresAt time.Time
}

func NewCache() *Cache {
	return &Cache{
		data: make(map[string]cacheEntry),
	}
}

func (c *Cache) Get(key string) (string, bool) {
	c.mu.RLock()
	entry, exists := c.data[key]
	c.mu.RUnlock()

	if !exists {
		return "", false
	}
	if time.Now().After(entry.expiresAt) {
		// 升级为写锁，删除过期 key，避免 map 无限增长
		c.mu.Lock()
		delete(c.data, key)
		c.mu.Unlock()
		return "", false
	}
	return entry.value, true
}

func (c *Cache) Set(key string, value string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.data[key] = cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
}

func NewProxyServer(cfg *Config) *ProxyServer {
	return &ProxyServer{
		cfg:    cfg,
		cache:  NewCache(),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (s *ProxyServer) ReloadClient() {
	embyHost := s.cfg.Server.Addr
	if !strings.HasPrefix(embyHost, "http://") && !strings.HasPrefix(embyHost, "https://") {
		embyHost = "http://" + embyHost
	}
	
	s.mediaServer = NewClient(s.cfg.Server.Type, embyHost, s.cfg.Server.Auth)
}

func (s *ProxyServer) ReverseProxy(c *gin.Context, modifyResponse bool) {
	hostUrl := s.cfg.Server.Addr
	if !strings.HasPrefix(hostUrl, "http://") && !strings.HasPrefix(hostUrl, "https://") {
		hostUrl = "http://" + hostUrl
	}

	remote, err := url.Parse(hostUrl)
	if err != nil {
		c.String(500, "Config Error: Invalid Server Addr")
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(remote)
	
	proxy.Transport = &http.Transport{
		ResponseHeaderTimeout: 30 * time.Second,
	}

	proxy.Director = func(req *http.Request) {
		req.Header = c.Request.Header
		req.Host = remote.Host
		req.URL.Scheme = remote.Scheme
		req.URL.Host = remote.Host
		req.URL.Path = c.Request.URL.Path
		req.URL.RawQuery = c.Request.URL.RawQuery
		
		if modifyResponse {
			req.Header.Del("Accept-Encoding")
		}
	}

	if modifyResponse {
		proxy.ModifyResponse = func(resp *http.Response) error {
			if resp.StatusCode != 200 {
				return nil
			}

			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}
			_ = resp.Body.Close()

			var respBody []byte
			if resp.Header.Get("Content-Encoding") == "gzip" {
				reader, err := gzip.NewReader(bytes.NewReader(bodyBytes))
				if err == nil {
					respBody, _ = io.ReadAll(reader)
					_ = reader.Close()
				} else {
					respBody = bodyBytes
				}
			} else {
				respBody = bodyBytes
			}

			newBody := s.modifyPlaybackInfo(respBody)

			resp.Header.Del("Content-Encoding")
			resp.Header.Del("Content-Length")
			resp.Body = io.NopCloser(bytes.NewReader(newBody))
			return nil
		}
	}

	proxy.ServeHTTP(c.Writer, c.Request)
}

func (s *ProxyServer) modifyPlaybackInfo(body []byte) []byte {
	if !s.cfg.HttpStrm.DisableTranscode {
		return body
	}

	jsonStr := string(body)
	
	sources := gjson.Get(jsonStr, "MediaSources").Array()
	for i := range sources {
		prefix := fmt.Sprintf("MediaSources.%d", i)
		jsonStr, _ = sjson.Set(jsonStr, prefix+".SupportsDirectPlay", true)
		jsonStr, _ = sjson.Set(jsonStr, prefix+".SupportsDirectStream", true)
		jsonStr, _ = sjson.Set(jsonStr, prefix+".SupportsTranscoding", false)

		dUrl := gjson.Get(jsonStr, prefix+".DirectStreamUrl").String()
		if dUrl != "" {
			sep := "?"
			if strings.Contains(dUrl, "?") {
				sep = "&"
			}
			dUrl += sep + "Emby2Alist=true"
			jsonStr, _ = sjson.Set(jsonStr, prefix+".DirectStreamUrl", dUrl)
		}
	}
	
	return []byte(jsonStr)
}