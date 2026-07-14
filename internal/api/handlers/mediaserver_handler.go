package handlers

import (
	"cloudstream/internal/database"
	"cloudstream/internal/mediaserver"
	"cloudstream/internal/models"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

var mediaServerMutationMu sync.Mutex

type mediaServerCreateRequest struct {
	Name             string `json:"Name"`
	ServerType       string `json:"ServerType"`
	ServerAddr       string `json:"ServerAddr"`
	APIKey           string `json:"APIKey"`
	CacheEnable      *bool  `json:"CacheEnable"`
	HttpStrmTTL      int    `json:"HttpStrmTTL"`
	ClientEnable     *bool  `json:"ClientEnable"`
	ClientMode       string `json:"ClientMode"`
	ClientList       string `json:"ClientList"`
	HttpStrmEnable   *bool  `json:"HttpStrmEnable"`
	DisableTranscode *bool  `json:"DisableTranscode"`
	ResolveStrmLinks *bool  `json:"ResolveStrmLinks"`
	UaPassthrough    *bool  `json:"UaPassthrough"`
	PathMappings     string `json:"PathMappings"`
	Enabled          *bool  `json:"Enabled"`
	Port             int    `json:"Port"`
}

func boolOrDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func ListMediaServersHandler(c *gin.Context) {
	var servers []models.MediaServer
	if err := database.DB.Order("id asc").Find(&servers).Error; err != nil {
		log.Error().Err(err).Msg("获取媒体服务器列表失败")
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "获取媒体服务器列表失败"})
		return
	}
	data := make([]gin.H, 0, len(servers))
	for _, server := range servers {
		data = append(data, sanitizeMediaServer(server))
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": data})
}

func sanitizeMediaServer(server models.MediaServer) gin.H {
	return gin.H{
		"ID": server.ID, "CreatedAt": server.CreatedAt, "UpdatedAt": server.UpdatedAt,
		"Name": server.Name, "ServerType": server.ServerType, "ServerAddr": server.ServerAddr,
		"CacheEnable": server.CacheEnable, "HttpStrmTTL": server.HttpStrmTTL,
		"ClientEnable": server.ClientEnable, "ClientMode": server.ClientMode, "ClientList": server.ClientList,
		"HttpStrmEnable": server.HttpStrmEnable, "DisableTranscode": server.DisableTranscode,
		"ResolveStrmLinks": server.ResolveStrmLinks, "UaPassthrough": server.UaPassthrough,
		"PathMappings": server.PathMappings, "Enabled": server.Enabled, "Port": mediaserver.ProxyPort,
		"HasAPIKey": server.APIKey != "",
	}
}

type revealMediaServerSecretRequest struct {
	Field      string `json:"field" binding:"required"`
	ServerType string `json:"serverType" binding:"required"`
	ServerAddr string `json:"serverAddr" binding:"required"`
	UpdatedAt  string `json:"updatedAt" binding:"required"`
}

func RevealMediaServerSecretHandler(c *gin.Context) {
	serverID, ok := parseMediaServerID(c)
	if !ok {
		return
	}
	var req revealMediaServerSecretRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Field != "api_key" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "凭据类型无效"})
		return
	}
	var server models.MediaServer
	if err := database.DB.First(&server, serverID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "媒体服务器未找到"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "读取媒体服务器凭据失败"})
		}
		return
	}
	if !validateSecretRevealTimestamp(c, req.UpdatedAt, server.UpdatedAt, "媒体服务器配置已发生变化，请刷新后重试") {
		return
	}
	if req.ServerType != server.ServerType || normalizeMediaServerAddress(req.ServerAddr) != normalizeMediaServerAddress(server.ServerAddr) {
		c.JSON(http.StatusConflict, gin.H{"code": 1, "message": "媒体服务器配置已发生变化，请刷新后重试"})
		return
	}
	respondSecretValue(c, req.Field, server.APIKey, "当前媒体服务器未保存 API Key")
}

func normalizeMediaServerAddress(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

func validateMediaServer(server *models.MediaServer) (bool, string) {
	if server.Name == "" {
		return false, "服务器名称不能为空"
	}
	if server.ServerType == "" {
		server.ServerType = "Emby"
	}
	if server.ServerType != "Emby" && server.ServerType != "Jellyfin" {
		return false, "服务器类型必须是 Emby 或 Jellyfin"
	}
	if server.ServerAddr == "" {
		return false, "服务器地址不能为空"
	}
	if err := mediaserver.ValidateServerAddress(server.ServerAddr); err != nil {
		return false, "服务器地址必须是有效的 HTTP 或 HTTPS 地址"
	}
	if server.APIKey == "" {
		return false, "API Key不能为空"
	}
	if server.HttpStrmTTL == 0 {
		server.HttpStrmTTL = 1
	}
	if server.HttpStrmTTL < 1 {
		return false, "HTTPStrm 缓存 TTL 必须大于 0"
	}
	if server.ClientMode == "" {
		server.ClientMode = "BlackList"
	}
	if server.ClientMode != "" && server.ClientMode != "WhiteList" && server.ClientMode != "BlackList" {
		return false, "客户端过滤模式必须是 WhiteList 或 BlackList"
	}
	if server.Port != 0 && server.Port != mediaserver.ProxyPort {
		return false, "媒体代理端口固定为 8091"
	}
	server.Port = mediaserver.ProxyPort
	if server.PathMappings == "" {
		server.PathMappings = "[]"
	}
	if server.ClientList == "" {
		server.ClientList = "[]"
	}
	if _, err := mediaserver.ParsePathMappings(server.PathMappings); err != nil {
		return false, "路径映射 JSON 格式错误"
	}
	if _, err := mediaserver.ParseClientList(server.ClientList); err != nil {
		return false, "客户端列表 JSON 格式错误"
	}
	return true, ""
}

func CreateMediaServerHandler(c *gin.Context) {
	var req mediaServerCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "参数错误"})
		return
	}
	server := models.MediaServer{
		Name:             req.Name,
		ServerType:       req.ServerType,
		ServerAddr:       req.ServerAddr,
		APIKey:           req.APIKey,
		CacheEnable:      boolOrDefault(req.CacheEnable, true),
		HttpStrmTTL:      req.HttpStrmTTL,
		ClientEnable:     boolOrDefault(req.ClientEnable, false),
		ClientMode:       req.ClientMode,
		ClientList:       req.ClientList,
		HttpStrmEnable:   boolOrDefault(req.HttpStrmEnable, true),
		DisableTranscode: boolOrDefault(req.DisableTranscode, true),
		ResolveStrmLinks: boolOrDefault(req.ResolveStrmLinks, true),
		UaPassthrough:    boolOrDefault(req.UaPassthrough, false),
		PathMappings:     req.PathMappings,
		Enabled:          boolOrDefault(req.Enabled, true),
		Port:             req.Port,
	}
	server.ServerAddr = normalizeMediaServerAddress(server.ServerAddr)
	server.APIKey = strings.TrimSpace(server.APIKey)

	if ok, msg := validateMediaServer(&server); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": msg})
		return
	}
	mediaServerMutationMu.Lock()
	defer mediaServerMutationMu.Unlock()

	// GORM 对 bool 零值会套用 default:true，创建后强制写回请求中的真实布尔值
	requested := server
	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&server).Error; err != nil {
			return err
		}
		return tx.Model(&server).Updates(map[string]interface{}{
			"cache_enable":       requested.CacheEnable,
			"client_enable":      requested.ClientEnable,
			"http_strm_enable":   requested.HttpStrmEnable,
			"disable_transcode":  requested.DisableTranscode,
			"resolve_strm_links": requested.ResolveStrmLinks,
			"ua_passthrough":     requested.UaPassthrough,
			"enabled":            requested.Enabled,
		}).Error
	}); err != nil {
		log.Error().Err(err).Msg("创建媒体服务器失败")
		if isMutationUniqueConstraintError(err) {
			c.JSON(http.StatusConflict, gin.H{"code": 1, "message": "媒体服务器名称已存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "创建媒体服务器失败"})
		return
	}
	server.CacheEnable = requested.CacheEnable
	server.ClientEnable = requested.ClientEnable
	server.HttpStrmEnable = requested.HttpStrmEnable
	server.DisableTranscode = requested.DisableTranscode
	server.ResolveStrmLinks = requested.ResolveStrmLinks
	server.UaPassthrough = requested.UaPassthrough
	server.Enabled = requested.Enabled

	if err := mediaserver.GetManager().ReloadServer(server.ID); err != nil {
		log.Error().Err(err).Uint("serverID", server.ID).Msg("媒体服务器已创建，但代理加载失败")
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": sanitizeMediaServer(server), "warning": "媒体服务器已创建，但代理加载失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": sanitizeMediaServer(server)})
}

type mediaServerUpdateRequest struct {
	Name             *string `json:"Name"`
	ServerType       *string `json:"ServerType"`
	ServerAddr       *string `json:"ServerAddr"`
	APIKey           *string `json:"APIKey"`
	CacheEnable      *bool   `json:"CacheEnable"`
	HttpStrmTTL      *int    `json:"HttpStrmTTL"`
	ClientEnable     *bool   `json:"ClientEnable"`
	ClientMode       *string `json:"ClientMode"`
	ClientList       *string `json:"ClientList"`
	HttpStrmEnable   *bool   `json:"HttpStrmEnable"`
	DisableTranscode *bool   `json:"DisableTranscode"`
	ResolveStrmLinks *bool   `json:"ResolveStrmLinks"`
	UaPassthrough    *bool   `json:"UaPassthrough"`
	PathMappings     *string `json:"PathMappings"`
	Enabled          *bool   `json:"Enabled"`
	Port             *int    `json:"Port"`
}

func UpdateMediaServerHandler(c *gin.Context) {
	serverID, ok := parseMediaServerID(c)
	if !ok {
		return
	}
	mediaServerMutationMu.Lock()
	defer mediaServerMutationMu.Unlock()

	var server models.MediaServer
	if err := database.DB.First(&server, serverID).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Error().Err(err).Uint("serverID", serverID).Msg("获取媒体服务器失败")
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "获取媒体服务器失败"})
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "媒体服务器未找到"})
		return
	}
	originalServerType := server.ServerType
	originalServerAddr := normalizeMediaServerAddress(server.ServerAddr)
	server.ServerAddr = originalServerAddr

	var req mediaServerUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "参数错误"})
		return
	}

	if req.Name != nil {
		server.Name = *req.Name
	}
	if req.ServerType != nil {
		server.ServerType = *req.ServerType
	}
	if req.ServerAddr != nil {
		server.ServerAddr = normalizeMediaServerAddress(*req.ServerAddr)
	}
	apiKeySubmitted := req.APIKey != nil && strings.TrimSpace(*req.APIKey) != ""
	if apiKeySubmitted {
		server.APIKey = strings.TrimSpace(*req.APIKey)
	}
	if (server.ServerType != originalServerType || server.ServerAddr != originalServerAddr) && !apiKeySubmitted {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "服务器地址或类型变化时必须重新提交 API Key"})
		return
	}
	if req.CacheEnable != nil {
		server.CacheEnable = *req.CacheEnable
	}
	if req.HttpStrmTTL != nil {
		server.HttpStrmTTL = *req.HttpStrmTTL
	}
	if req.ClientEnable != nil {
		server.ClientEnable = *req.ClientEnable
	}
	if req.ClientMode != nil {
		server.ClientMode = *req.ClientMode
	}
	if req.ClientList != nil {
		server.ClientList = *req.ClientList
	}
	if req.HttpStrmEnable != nil {
		server.HttpStrmEnable = *req.HttpStrmEnable
	}
	if req.DisableTranscode != nil {
		server.DisableTranscode = *req.DisableTranscode
	}
	if req.ResolveStrmLinks != nil {
		server.ResolveStrmLinks = *req.ResolveStrmLinks
	}
	if req.UaPassthrough != nil {
		server.UaPassthrough = *req.UaPassthrough
	}
	if req.PathMappings != nil {
		server.PathMappings = *req.PathMappings
	}
	if req.Enabled != nil {
		server.Enabled = *req.Enabled
	}
	if req.Port != nil {
		server.Port = *req.Port
	} else {
		server.Port = mediaserver.ProxyPort
	}

	if ok, msg := validateMediaServer(&server); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": msg})
		return
	}

	updates := map[string]interface{}{
		"name":               server.Name,
		"server_type":        server.ServerType,
		"server_addr":        server.ServerAddr,
		"api_key":            server.APIKey,
		"cache_enable":       server.CacheEnable,
		"http_strm_ttl":      server.HttpStrmTTL,
		"client_enable":      server.ClientEnable,
		"client_mode":        server.ClientMode,
		"client_list":        server.ClientList,
		"http_strm_enable":   server.HttpStrmEnable,
		"disable_transcode":  server.DisableTranscode,
		"resolve_strm_links": server.ResolveStrmLinks,
		"ua_passthrough":     server.UaPassthrough,
		"path_mappings":      server.PathMappings,
		"enabled":            server.Enabled,
		"port":               mediaserver.ProxyPort,
	}
	result := database.DB.Model(&models.MediaServer{}).Where("id = ?", serverID).Updates(updates)
	if result.Error != nil {
		log.Error().Err(result.Error).Uint("serverID", serverID).Msg("更新媒体服务器失败")
		if isMutationUniqueConstraintError(result.Error) {
			c.JSON(http.StatusConflict, gin.H{"code": 1, "message": "媒体服务器名称已存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "更新媒体服务器失败"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "媒体服务器未找到"})
		return
	}
	if err := database.DB.First(&server, serverID).Error; err != nil {
		log.Error().Err(err).Uint("serverID", serverID).Msg("重新读取媒体服务器失败")
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "更新媒体服务器失败"})
		return
	}
	server.Port = mediaserver.ProxyPort

	if err := mediaserver.GetManager().ReloadServer(server.ID); err != nil {
		log.Error().Err(err).Uint("serverID", server.ID).Msg("媒体服务器已更新，但代理加载失败")
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": sanitizeMediaServer(server), "warning": "媒体服务器已更新，但代理加载失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": sanitizeMediaServer(server)})
}

func DeleteMediaServerHandler(c *gin.Context) {
	serverID, ok := parseMediaServerID(c)
	if !ok {
		return
	}
	mediaServerMutationMu.Lock()
	defer mediaServerMutationMu.Unlock()

	result := database.DB.Unscoped().Where("id = ?", serverID).Delete(&models.MediaServer{})
	if result.Error != nil {
		log.Error().Err(result.Error).Uint("serverID", serverID).Msg("删除媒体服务器失败")
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "删除媒体服务器失败"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "媒体服务器未找到"})
		return
	}

	if err := mediaserver.GetManager().ReloadServer(serverID); err != nil {
		log.Error().Err(err).Uint("serverID", serverID).Msg("媒体服务器已删除，但代理卸载失败")
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "媒体服务器已删除", "warning": "代理卸载失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "媒体服务器已删除"})
}

func TestMediaServerConnectionHandler(c *gin.Context) {
	var server models.MediaServer
	if err := c.ShouldBindJSON(&server); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "参数错误"})
		return
	}
	server.ServerAddr = normalizeMediaServerAddress(server.ServerAddr)
	server.APIKey = strings.TrimSpace(server.APIKey)
	if server.ID != 0 && server.APIKey == "" {
		if uint64(server.ID) > uint64(^uint32(0)) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "无效的媒体服务器ID"})
			return
		}
		var stored models.MediaServer
		if err := database.DB.First(&stored, server.ID).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				log.Error().Err(err).Uint("serverID", server.ID).Msg("获取媒体服务器失败")
				c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "获取媒体服务器失败"})
				return
			}
			c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "媒体服务器未找到"})
			return
		}
		if server.ServerType != stored.ServerType || normalizeMediaServerAddress(server.ServerAddr) != normalizeMediaServerAddress(stored.ServerAddr) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "服务器地址或类型变化时必须重新提交 API Key"})
			return
		}
		server.APIKey = stored.APIKey
	}

	if ok, msg := validateMediaServer(&server); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": msg})
		return
	}

	if err := mediaserver.TestConnection(&server); err != nil {
		log.Warn().Err(err).Str("server", server.Name).Msg("媒体服务器连接测试失败")
		c.JSON(http.StatusBadGateway, gin.H{"code": 1, "message": "媒体服务器连接或鉴权失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "连接成功"})
}

func MediaServerProxyHandler(c *gin.Context) {
	serverID, ok := parseMediaServerID(c)
	if !ok {
		return
	}

	upstreamPath := c.Param("path")
	const routePrefix = "/api/v1/ms/"
	if escapedPath := c.Request.URL.EscapedPath(); strings.HasPrefix(escapedPath, routePrefix) {
		remainder := strings.TrimPrefix(escapedPath, routePrefix)
		if slash := strings.IndexByte(remainder, '/'); slash >= 0 {
			upstreamPath = remainder[slash:]
		} else {
			upstreamPath = "/"
		}
	}

	mediaserver.GetManager().HandleProxy(c, serverID, upstreamPath)
}

func parseMediaServerID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "无效的媒体服务器ID"})
		return 0, false
	}
	return uint(id), true
}
