package mediaserver

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// Manager 管理所有媒体服务器实例
type Manager struct {
	servers map[uint]*ProxyServer
	mu      sync.RWMutex
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
	m.mu.Lock()
	defer m.mu.Unlock()

	var servers []models.MediaServer
	if err := database.DB.Find(&servers).Error; err != nil {
		return err
	}

	// 清空现有服务器
	for id := range m.servers {
		delete(m.servers, id)
	}

	// 重新加载
	for _, server := range servers {
		if server.Enabled {
			cfg := m.modelToConfig(&server)
			proxy := NewProxyServer(cfg)
			proxy.ReloadClient()
			m.servers[server.ID] = proxy
			log.Info().Uint("id", server.ID).Str("name", server.Name).Msg("媒体服务器已加载")
		}
	}

	return nil
}

// ReloadServer 重新加载单个服务器
func (m *Manager) ReloadServer(id uint) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var server models.MediaServer
	if err := database.DB.First(&server, id).Error; err != nil {
		return err
	}

	// 删除旧实例
	delete(m.servers, id)

	// 如果启用，创建新实例
	if server.Enabled {
		cfg := m.modelToConfig(&server)
		proxy := NewProxyServer(cfg)
		proxy.ReloadClient()
		m.servers[id] = proxy
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

// modelToConfig 将数据库模型转换为配置
func (m *Manager) modelToConfig(server *models.MediaServer) *Config {
	pathMappings, _ := ParsePathMappings(server.PathMappings)
	clientList, _ := ParseClientList(server.ClientList)

	return &Config{
		Port: server.Port,
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
func (m *Manager) HandleProxy(c *gin.Context, serverID uint) {
	proxy, exists := m.GetServer(serverID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "媒体服务器未找到或未启用"})
		return
	}

	cfg := proxy.cfg

	// 客户端过滤
	if cfg.Client.Enable {
		userAgent := c.Request.UserAgent()
		allowed := false
		for _, client := range cfg.Client.List {
			if strings.Contains(userAgent, client) {
				allowed = true
				break
			}
		}

		if cfg.Client.Mode == "WhiteList" {
			if !allowed {
				c.JSON(http.StatusForbidden, gin.H{"code": 1, "message": "客户端不在白名单中"})
				return
			}
		} else if cfg.Client.Mode == "BlackList" {
			if allowed {
				c.JSON(http.StatusForbidden, gin.H{"code": 1, "message": "客户端在黑名单中"})
				return
			}
		}
	}

	// PlaybackInfo 拦截
	if strings.Contains(c.Request.URL.Path, "/PlaybackInfo") {
		log.Info().Uint("server_id", serverID).Str("client_ip", c.ClientIP()).Msg("拦截 PlaybackInfo 请求")
		proxy.ReverseProxy(c, true)
		return
	}

	// 识别流媒体请求
	path := c.Request.URL.Path
	isStream := strings.Contains(path, "/stream") || strings.Contains(path, "/Download") || strings.Contains(path, "/original")

	if !isStream {
		proxy.ReverseProxy(c, false)
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
		proxy.ReverseProxy(c, false)
		return
	}

	// 处理 HTTPStrm
	if strings.HasPrefix(realPath, "http") {
		if !cfg.HttpStrm.Enable {
			proxy.ReverseProxy(c, false)
			return
		}

		// 缓存检查
		cacheKey := fmt.Sprintf("strm:%d:%s", serverID, realPath)
		if cfg.Cache.Enable {
			if cachedURL, found := proxy.cache.Get(cacheKey); found {
				log.Info().Str("cached_url", cachedURL).Msg("缓存命中，直接跳转")
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

		// 自动解析 302
		if cfg.HttpStrm.ResolveStrmLinks {
			log.Info().Str("url", targetPath).Msg("开始解析 Strm 链接")
			req, _ := http.NewRequest("GET", targetPath, nil)

			// UA 透传
			if cfg.HttpStrm.UaPassthrough {
				req.Header.Set("User-Agent", c.Request.UserAgent())
			} else {
				req.Header.Set("User-Agent", "Mozilla/5.0")
			}

			resp, err := proxy.httpClient.Do(req)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode >= 300 && resp.StatusCode < 400 {
					loc := resp.Header.Get("Location")
					if loc != "" {
						log.Info().Str("location", loc).Msg("解析成功")
						targetPath = loc

						// 写入缓存
						if cfg.Cache.Enable {
							ttl := time.Duration(cfg.Cache.HttpStrmTTL) * time.Minute
							proxy.cache.Set(cacheKey, targetPath, ttl)
							log.Info().Dur("ttl", ttl).Msg("已缓存直链")
						}
					}
				}
			} else {
				log.Warn().Err(err).Msg("解析失败")
			}
		} else {
			// 如果没有开启解析302，但开启了缓存，也缓存替换后的路径
			if cfg.Cache.Enable {
				ttl := time.Duration(cfg.Cache.HttpStrmTTL) * time.Minute
				proxy.cache.Set(cacheKey, targetPath, ttl)
			}
		}

		log.Info().Str("target", targetPath).Msg("跳转到直链")
		c.Redirect(http.StatusFound, targetPath)
		return
	}

	// 如果不是 http 开头，直接代理回源
	proxy.ReverseProxy(c, false)
}

// TestConnection 测试媒体服务器连接
func TestConnection(server *models.MediaServer) error {
	cfg := &Config{
		Server: ServerConf{
			Type: server.ServerType,
			Addr: server.ServerAddr,
			Auth: server.APIKey,
		},
	}

	client := NewClient(cfg.Server.Type, cfg.Server.Addr, cfg.Server.Auth)
	
	// 尝试获取系统信息来测试连接
	_, err := client.GetItemInfo("", "")
	if err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}

	return nil
}