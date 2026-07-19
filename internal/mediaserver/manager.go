package mediaserver

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
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
	if strings.Contains(strings.ToLower(path), "/playbackinfo") {
		log.Info().Uint("server_id", serverID).Str("client_ip", c.ClientIP()).Msg("拦截 PlaybackInfo 请求")
		proxy.ReverseProxy(c, upstreamPath, cfg.HttpStrm.Enable)
		return
	}

	// 识别流媒体请求
	lowerPath := strings.ToLower(path)
	isStream := strings.Contains(lowerPath, "/stream") || strings.Contains(lowerPath, "/download") || strings.Contains(lowerPath, "/original")

	if !isStream {
		proxy.ReverseProxy(c, upstreamPath, false)
		return
	}
	if !cfg.HttpStrm.Enable || (c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead) {
		proxy.ReverseProxy(c, upstreamPath, false)
		return
	}

	itemId := extractItemId(path)
	if itemId == "" {
		proxy.ReverseProxy(c, upstreamPath, false)
		return
	}
	accessToken := requestAccessToken(c)
	if accessToken == "" {
		proxy.ReverseProxy(c, upstreamPath, false)
		return
	}
	mediaSourceId := c.Query("MediaSourceId")
	if mediaSourceId == "" {
		mediaSourceId = c.Query("mediaSourceId")
	}

	log.Info().Uint("server_id", serverID).Str("item_id", itemId).Str("source_id", mediaSourceId).Msg("接收到播放请求")

	itemInfo, err := proxy.mediaServer.GetItemInfo(itemId, mediaSourceId, accessToken)
	if err != nil {
		log.Error().Err(err).Uint("server_id", serverID).Msg("获取路径失败，回源代理")
		proxy.ReverseProxy(c, upstreamPath, false)
		return
	}
	if !itemInfo.IsHTTPStrm() {
		proxy.ReverseProxy(c, upstreamPath, false)
		return
	}
	realPath := itemInfo.MediaPath

	// 处理 HTTPStrm
	realURL, err := url.Parse(realPath)
	if err == nil && isHTTPURL(realURL) {
		// 缓存检查
		cacheKey := fmt.Sprintf("strm:%d:%s:%s", serverID, c.Request.Method, realPath)
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

		targetPath := applyPathMappings(realPath, cfg.HttpStrm.PathMappings)

		if cfg.HttpStrm.ResolveStrmLinks {
			targetPath = proxy.resolveHTTPStrm(c.Request.Context(), cacheKey, targetPath, c.Request.Method, c.Request.UserAgent())
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

func (s *ProxyServer) resolveHTTPStrm(ctx context.Context, cacheKey, targetPath, method, userAgent string) string {
	s.resolveMu.Lock()
	if flight, exists := s.resolveFlights[cacheKey]; exists {
		s.resolveMu.Unlock()
		select {
		case <-flight.done:
			return flight.target
		case <-ctx.Done():
			return targetPath
		}
	}
	flight := &resolveFlight{done: make(chan struct{}), target: targetPath}
	s.resolveFlights[cacheKey] = flight
	s.resolveMu.Unlock()

	defer func() {
		s.resolveMu.Lock()
		flight.target = targetPath
		close(flight.done)
		delete(s.resolveFlights, cacheKey)
		s.resolveMu.Unlock()
	}()

	parsedTarget, err := url.Parse(targetPath)
	if err != nil || !isHTTPURL(parsedTarget) {
		log.Warn().Str("target", urlForLog(targetPath)).Str("error", errorWithoutURL(err)).Msg("Strm 链接无效")
		return targetPath
	}
	resolveCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	allowPrivateRedirects, err := URLTargetsPrivateNetwork(resolveCtx, parsedTarget)
	if err != nil {
		log.Warn().Str("target", urlForLog(targetPath)).Str("error", errorWithoutURL(err)).Msg("无法验证 Strm 链接目标")
		return targetPath
	}
	resolveClient, closeResolveClient := s.resolveClient(allowPrivateRedirects)
	defer closeResolveClient()

	log.Info().Str("target", urlForLog(targetPath)).Msg("开始解析 Strm 链接")
	currentURL := targetPath
	visited := make(map[string]struct{}, 10)
	redirected := false
	redirects := 0
	for {
		if _, exists := visited[currentURL]; exists {
			log.Warn().Msg("Strm 链接存在循环重定向")
			return targetPath
		}
		visited[currentURL] = struct{}{}

		req, err := http.NewRequestWithContext(resolveCtx, method, currentURL, nil)
		if err != nil {
			log.Warn().Str("error", errorWithoutURL(err)).Msg("创建 Strm 解析请求失败")
			return targetPath
		}
		if s.cfg.HttpStrm.UaPassthrough {
			req.Header.Set("User-Agent", userAgent)
		} else {
			req.Header.Set("User-Agent", "Mozilla/5.0")
		}
		resp, err := resolveClient.Do(req)
		if err != nil {
			log.Warn().Str("method", method).Str("error", errorWithoutURL(err)).Msg("解析失败")
			return targetPath
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 32<<10))
		_ = resp.Body.Close()
		if !isRedirectStatus(resp.StatusCode) {
			if !redirected || resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
				log.Warn().Str("method", method).Int("status", resp.StatusCode).Msg("Strm 链接未返回重定向")
				return targetPath
			}
			targetPath = currentURL
			break
		}
		location, err := resp.Location()
		if err != nil || !isHTTPURL(location) {
			log.Warn().Str("method", method).Str("error", errorWithoutURL(err)).Msg("Strm 重定向地址无效")
			return targetPath
		}
		if redirects >= 10 {
			log.Warn().Msg("Strm 链接重定向次数超过限制")
			return targetPath
		}
		if !allowPrivateRedirects {
			private, err := URLTargetsPrivateNetwork(resolveCtx, location)
			if err != nil || private {
				log.Warn().Str("target", urlForLog(location.String())).Str("error", errorWithoutURL(err)).Msg("拒绝 Strm 链接重定向到私有网络")
				return targetPath
			}
		}
		currentURL = location.String()
		redirected = true
		redirects++
	}
	log.Info().Str("method", method).Str("target", urlForLog(targetPath)).Msg("解析成功")
	if s.cfg.Cache.Enable {
		ttl := time.Duration(s.cfg.Cache.HttpStrmTTL) * time.Minute
		s.cache.Set(cacheKey, targetPath, ttl)
		log.Info().Dur("ttl", ttl).Msg("已缓存直链")
	}
	return targetPath
}

func (s *ProxyServer) resolveClient(allowPrivate bool) (*http.Client, func()) {
	if allowPrivate {
		return s.httpClient, func() {}
	}
	transport := s.transport.Clone()
	transport.Proxy = nil
	transport.DialContext = safePublicDialContext
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return client, transport.CloseIdleConnections
}

func safePublicDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	var lastErr error
	for _, address := range addresses {
		if isPrivateNetworkIP(address.IP) {
			continue
		}
		connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
		if err == nil {
			return connection, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("target resolves only to private network addresses")
}

func URLTargetsPrivateNetwork(ctx context.Context, target *url.URL) (bool, error) {
	if !isHTTPURL(target) {
		return false, fmt.Errorf("invalid HTTP URL")
	}
	host := target.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true, nil
	}
	if ip := net.ParseIP(host); ip != nil {
		return isPrivateNetworkIP(ip), nil
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return false, err
	}
	if len(addresses) == 0 {
		return false, fmt.Errorf("target has no IP addresses")
	}
	for _, address := range addresses {
		if isPrivateNetworkIP(address.IP) {
			return true, nil
		}
	}
	return false, nil
}

func isPrivateNetworkIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	for _, network := range nonPublicNetworks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

var nonPublicNetworks = mustParseCIDRs(
	"100.64.0.0/10",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"2001:db8::/32",
)

func mustParseCIDRs(values ...string) []*net.IPNet {
	networks := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			panic(err)
		}
		networks = append(networks, network)
	}
	return networks
}

func allowProxyClient(c *gin.Context, cfg ClientFilterConf) bool {
	if !cfg.Enable {
		return true
	}

	userAgent := strings.ToLower(c.Request.UserAgent())
	matched := false
	for _, client := range cfg.List {
		client = strings.ToLower(strings.TrimSpace(client))
		if client != "" && strings.Contains(userAgent, client) {
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

func requestAccessToken(c *gin.Context) string {
	for _, header := range []string{"X-Emby-Token", "X-MediaBrowser-Token"} {
		if token := strings.TrimSpace(c.GetHeader(header)); token != "" {
			return token
		}
	}
	for key, values := range c.Request.URL.Query() {
		if isMediaTokenQueryKey(key) {
			for _, value := range values {
				if token := strings.TrimSpace(value); token != "" {
					return token
				}
			}
		}
	}
	if authorization := strings.TrimSpace(c.GetHeader("Authorization")); strings.HasPrefix(strings.ToLower(authorization), "bearer ") {
		return strings.TrimSpace(authorization[len("Bearer "):])
	}
	if authorization := c.GetHeader("X-Emby-Authorization"); authorization != "" {
		for _, part := range strings.Split(authorization, ",") {
			key, value, found := strings.Cut(strings.TrimSpace(part), "=")
			if found && strings.EqualFold(strings.TrimSpace(key), "Token") {
				return strings.Trim(strings.TrimSpace(value), `"`)
			}
		}
	}
	return ""
}

func isMediaTokenQueryKey(key string) bool {
	return strings.EqualFold(key, "api_key") || strings.EqualFold(key, "apiKey") || strings.EqualFold(key, "token") || strings.EqualFold(key, "X-Emby-Token")
}

func applyPathMappings(target string, mappings []PathMapping) string {
	for _, mapping := range mappings {
		oldPrefix := strings.TrimRight(strings.TrimSpace(mapping.Old), "/")
		newPrefix := strings.TrimRight(strings.TrimSpace(mapping.New), "/")
		if oldPrefix == "" || !strings.HasPrefix(target, oldPrefix) {
			continue
		}
		remainder := strings.TrimPrefix(target, oldPrefix)
		if remainder == "" || strings.HasPrefix(remainder, "/") || strings.HasPrefix(remainder, "?") || strings.HasPrefix(remainder, "#") {
			return newPrefix + remainder
		}
	}
	return target
}

func isRedirectStatus(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
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
