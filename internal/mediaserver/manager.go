package mediaserver

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// Manager 管理所有媒体服务器实例
type Manager struct {
	servers  map[uint]*ProxyServer
	mu       sync.RWMutex
	reloadMu sync.Mutex
	closed   bool
	retired  sync.WaitGroup
}

var globalManager *Manager
var managerOnce sync.Once

// GetManager 获取全局管理器实例
func GetManager() *Manager {
	managerOnce.Do(func() {
		globalManager = &Manager{
			servers: make(map[uint]*ProxyServer),
		}
	})
	return globalManager
}

// ReloadAll 从数据库重新加载所有配置
func (m *Manager) ReloadAll() error {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()
	if m.closed {
		return fmt.Errorf("media server manager is closed")
	}

	var servers []models.MediaServer
	if err := database.DB.Find(&servers).Error; err != nil {
		return err
	}

	loaded := make(map[uint]*ProxyServer)
	var loadErrors []error
	for _, server := range servers {
		if server.Enabled {
			cfg := m.modelToConfig(&server)
			proxy := NewProxyServer(cfg)
			if proxy.configErr != nil {
				proxy.Close()
				loadErrors = append(loadErrors, fmt.Errorf("load media server %d: %w", server.ID, proxy.configErr))
				log.Error().Err(proxy.configErr).Uint("id", server.ID).Str("name", server.Name).Msg("媒体服务器配置无效，已跳过")
				continue
			}
			proxy.ReloadClient()
			loaded[server.ID] = proxy
			log.Info().Uint("id", server.ID).Str("name", server.Name).Msg("媒体服务器已加载")
		}
	}

	m.mu.Lock()
	oldServers := m.servers
	m.servers = loaded
	m.mu.Unlock()

	for _, proxy := range oldServers {
		m.retireProxy(proxy)
	}

	return errors.Join(loadErrors...)
}

// ReloadServer 重新加载单个服务器（删除时 id 不存在也清理旧实例）
func (m *Manager) ReloadServer(id uint) error {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()
	if m.closed {
		return fmt.Errorf("media server manager is closed")
	}

	var server models.MediaServer
	if err := database.DB.First(&server, id).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		m.mu.Lock()
		oldProxy := m.servers[id]
		delete(m.servers, id)
		m.mu.Unlock()
		if oldProxy != nil {
			m.retireProxy(oldProxy)
		}
		log.Info().Uint("id", id).Msg("媒体服务器已从管理器卸载（记录不存在）")
		return nil
	}

	var newProxy *ProxyServer
	if server.Enabled {
		cfg := m.modelToConfig(&server)
		newProxy = NewProxyServer(cfg)
		if newProxy.configErr != nil {
			newProxy.Close()
			return newProxy.configErr
		}
		newProxy.ReloadClient()
	}

	m.mu.Lock()
	oldProxy := m.servers[id]
	if newProxy == nil {
		delete(m.servers, id)
	} else {
		m.servers[id] = newProxy
	}
	m.mu.Unlock()

	if oldProxy != nil {
		m.retireProxy(oldProxy)
	}
	if newProxy != nil {
		log.Info().Uint("id", server.ID).Str("name", server.Name).Msg("媒体服务器已重新加载")
	}

	return nil
}

// GetServer 获取指定服务器的代理实例
func (m *Manager) GetServer(id uint) (*ProxyServer, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	server, exists := m.servers[id]
	return server, exists
}

// GetFirstEnabledServerID 获取 ID 最小的启用媒体服务器
func (m *Manager) GetFirstEnabledServerID() uint {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var firstID uint
	for id := range m.servers {
		if firstID == 0 || id < firstID {
			firstID = id
		}
	}
	return firstID
}

// CloseAll stops all proxy-local background workers and closes idle upstream connections.
func (m *Manager) CloseAll() {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()
	if m.closed {
		return
	}
	m.closed = true

	m.mu.Lock()
	servers := m.servers
	m.servers = make(map[uint]*ProxyServer)
	m.mu.Unlock()

	for _, proxy := range servers {
		proxy.Close()
	}
	m.retired.Wait()
}

func (m *Manager) retireProxy(proxy *ProxyServer) {
	if proxy == nil {
		return
	}
	m.retired.Add(1)
	done := proxy.Retire()
	go func() {
		defer m.retired.Done()
		<-done
	}()
}

func (m *Manager) acquireServer(id uint) (*ProxyServer, bool) {
	m.mu.RLock()
	proxy, exists := m.servers[id]
	if exists {
		exists = proxy.beginRequest()
	}
	m.mu.RUnlock()
	return proxy, exists
}

func ValidateServerAddress(addr string) error {
	_, err := parseServerURL(addr)
	return err
}

// modelToConfig 将数据库模型转换为配置
func (m *Manager) modelToConfig(server *models.MediaServer) *Config {
	pathMappings, err := ParsePathMappings(server.PathMappings)
	if err != nil {
		log.Warn().Err(err).Uint("id", server.ID).Str("name", server.Name).Msg("媒体服务器路径映射配置无效，已按空列表处理")
		pathMappings = []PathMapping{}
	}
	clientList, err := ParseClientList(server.ClientList)
	if err != nil {
		log.Warn().Err(err).Uint("id", server.ID).Str("name", server.Name).Msg("媒体服务器客户端列表配置无效，已按空列表处理")
		clientList = []string{}
	}

	return &Config{
		Port: ProxyPort,
		Server: ServerConf{
			Type: server.ServerType,
			Addr: server.ServerAddr,
			Auth: server.APIKey,
		},
		Cache: CacheConf{
			Enable:      server.CacheEnable,
			HttpStrmTTL: server.HttpStrmTTL,
		},
		Client: ClientFilterConf{
			Enable: server.ClientEnable,
			Mode:   server.ClientMode,
			List:   clientList,
		},
		HttpStrm: HttpStrmConf{
			Enable:           server.HttpStrmEnable,
			DisableTranscode: server.DisableTranscode,
			ResolveStrmLinks: server.ResolveStrmLinks,
			UaPassthrough:    server.UaPassthrough,
			PathMappings:     pathMappings,
		},
	}
}

// extractItemId 从路径中提取ItemID
func extractItemId(path string) string {
	re := regexp.MustCompile(`/(?:videos|items)/([a-zA-Z0-9\-]+)/`)
	matches := re.FindStringSubmatch(strings.ToLower(path))
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

// HandleProxy 处理代理请求
func (m *Manager) HandleProxy(c *gin.Context, serverID uint, upstreamPath string) {
	proxy, exists := m.acquireServer(serverID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "媒体服务器未找到或未启用"})
		return
	}
	defer proxy.endRequest()

	cfg := proxy.cfg

	if !allowProxyClient(c, cfg.Client) {
		return
	}

	if isWebSocketRequest(c) {
		handleWebSocket(c, proxy, upstreamPath)
		return
	}

	path := upstreamPath
	if decodedPath, err := url.PathUnescape(upstreamPath); err == nil {
		path = decodedPath
	}

	// PlaybackInfo 拦截
	if strings.Contains(path, "/PlaybackInfo") {
		log.Info().Uint("server_id", serverID).Str("client_ip", c.ClientIP()).Msg("拦截 PlaybackInfo 请求")
		proxy.ReverseProxy(c, upstreamPath, true)
		return
	}

	// 识别流媒体请求
	isStream := strings.Contains(path, "/stream") || strings.Contains(path, "/Download") || strings.Contains(path, "/original")

	if !isStream {
		proxy.ReverseProxy(c, upstreamPath, false)
		return
	}

	itemId := extractItemId(path)
	mediaSourceId := c.Query("MediaSourceId")
	if mediaSourceId == "" {
		mediaSourceId = c.Query("mediaSourceId")
	}

	log.Info().Uint("server_id", serverID).Str("item_id", itemId).Str("source_id", mediaSourceId).Msg("接收到播放请求")

	realPath, err := proxy.mediaServer.GetItemInfo(itemId, mediaSourceId)
	if err != nil {
		log.Error().Err(err).Uint("server_id", serverID).Msg("获取路径失败，回源代理")
		proxy.ReverseProxy(c, upstreamPath, false)
		return
	}

	// 处理 HTTPStrm
	realURL, err := url.Parse(realPath)
	if err == nil && isHTTPURL(realURL) {
		if !cfg.HttpStrm.Enable {
			proxy.ReverseProxy(c, upstreamPath, false)
			return
		}

		// 缓存检查
		cacheKey := fmt.Sprintf("strm:%d:%s", serverID, realPath)
		if cfg.HttpStrm.UaPassthrough {
			userAgentHash := sha256.Sum256([]byte(c.Request.UserAgent()))
			cacheKey += fmt.Sprintf(":ua:%x", userAgentHash[:8])
		}
		if cfg.Cache.Enable {
			if cachedURL, found := proxy.cache.Get(cacheKey); found {
				log.Info().Str("target", urlForLog(cachedURL)).Msg("缓存命中，直接跳转")
				c.Redirect(http.StatusFound, cachedURL)
				return
			}
		}

		targetPath := realPath
		// 路径替换
		for _, m := range cfg.HttpStrm.PathMappings {
			if m.Old != "" {
				targetPath = strings.Replace(targetPath, m.Old, m.New, 1)
			}
		}

		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, targetPath, nil)
		if err != nil {
			log.Warn().
				Str("target", urlForLog(targetPath)).
				Str("error", errorWithoutURL(err)).
				Msg("Strm 链接无效，回源代理")
			proxy.ReverseProxy(c, upstreamPath, false)
			return
		}
		if !isHTTPURL(req.URL) {
			log.Warn().Str("target", urlForLog(targetPath)).Msg("Strm 链接协议不受支持，回源代理")
			proxy.ReverseProxy(c, upstreamPath, false)
			return
		}

		// UA 透传
		if cfg.HttpStrm.UaPassthrough {
			req.Header.Set("User-Agent", c.Request.UserAgent())
		} else {
			req.Header.Set("User-Agent", "Mozilla/5.0")
		}

		// 自动解析 302
		if cfg.HttpStrm.ResolveStrmLinks {
			log.Info().Str("target", urlForLog(targetPath)).Msg("开始解析 Strm 链接")

			resp, err := proxy.httpClient.Do(req)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode >= 300 && resp.StatusCode < 400 {
					location, locationErr := resp.Location()
					if locationErr == nil && isHTTPURL(location) {
						targetPath = location.String()
						log.Info().Str("target", urlForLog(targetPath)).Msg("解析成功")

						// 写入缓存
						if cfg.Cache.Enable {
							ttl := time.Duration(cfg.Cache.HttpStrmTTL) * time.Minute
							proxy.cache.Set(cacheKey, targetPath, ttl)
							log.Info().Dur("ttl", ttl).Msg("已缓存直链")
						}
					} else if locationErr != nil {
						log.Warn().Str("error", errorWithoutURL(locationErr)).Msg("Strm 重定向地址无效")
					} else {
						log.Warn().Str("target", urlForLog(location.String())).Msg("Strm 重定向协议不受支持")
					}
				} else {
					log.Warn().Int("status", resp.StatusCode).Msg("Strm 链接未返回重定向")
				}
			} else {
				log.Warn().Str("error", errorWithoutURL(err)).Msg("解析失败")
			}
		} else {
			// 如果没有开启解析302，但开启了缓存，也缓存替换后的路径
			if cfg.Cache.Enable {
				ttl := time.Duration(cfg.Cache.HttpStrmTTL) * time.Minute
				proxy.cache.Set(cacheKey, targetPath, ttl)
			}
		}

		log.Info().Str("target", urlForLog(targetPath)).Msg("跳转到直链")
		c.Redirect(http.StatusFound, targetPath)
		return
	}

	// 如果不是 http 开头，直接代理回源
	proxy.ReverseProxy(c, upstreamPath, false)
}

func allowProxyClient(c *gin.Context, cfg ClientFilterConf) bool {
	if !cfg.Enable {
		return true
	}

	userAgent := c.Request.UserAgent()
	matched := false
	for _, client := range cfg.List {
		if strings.Contains(userAgent, client) {
			matched = true
			break
		}
	}

	allowed := cfg.Mode != "WhiteList" || matched
	if cfg.Mode == "BlackList" && matched {
		allowed = false
	}
	if allowed {
		return true
	}

	log.Warn().
		Str("mode", cfg.Mode).
		Str("client_ip", c.ClientIP()).
		Str("user_agent", userAgent).
		Msg("客户端过滤拒绝访问")
	c.AbortWithStatus(http.StatusForbidden)
	return false
}

func isHTTPURL(target *url.URL) bool {
	if target == nil || target.Host == "" {
		return false
	}
	scheme := strings.ToLower(target.Scheme)
	return scheme == "http" || scheme == "https"
}

// TestConnection 使用需要鉴权的轻量端点验证连接和 API Key。
func TestConnection(server *models.MediaServer) error {
	serverURL, err := parseServerURL(server.ServerAddr)
	if err != nil {
		return err
	}
	serverURL.RawQuery = ""
	serverURL.ForceQuery = false
	serverURL.Fragment = ""
	client := NewClient(server.ServerType, serverURL.String(), server.APIKey)
	if closer, ok := client.(interface{ Close() }); ok {
		defer closer.Close()
	}
	return client.Ping()
}
