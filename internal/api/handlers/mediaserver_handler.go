package handlers

import (
	"cloudstream/internal/database"
	"cloudstream/internal/mediaserver"
	"cloudstream/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
	"strconv"
)

func ListMediaServersHandler(c *gin.Context) {
	var servers []models.MediaServer
	if err := database.DB.Order("id asc").Find(&servers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "获取媒体服务器列表失败: " + err.Error()})
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
		"PathMappings": server.PathMappings, "Enabled": server.Enabled, "Port": server.Port,
		"HasAPIKey": server.APIKey != "",
	}
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
	if server.Port == 0 {
		server.Port = 8091
	}
	if server.Port < 0 || server.Port > 65535 {
		return false, "端口范围必须是 0 到 65535"
	}
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
	var server models.MediaServer
	if err := c.ShouldBindJSON(&server); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}

	if ok, msg := validateMediaServer(&server); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": msg})
		return
	}

	requested := server
	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&server).Error; err != nil {
			return err
		}
		return tx.Model(&server).Updates(map[string]interface{}{
			"cache_enable": requested.CacheEnable, "http_strm_enable": requested.HttpStrmEnable,
			"disable_transcode": requested.DisableTranscode, "resolve_strm_links": requested.ResolveStrmLinks,
			"enabled": requested.Enabled,
		}).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "创建媒体服务器失败: " + err.Error()})
		return
	}
	server.CacheEnable = requested.CacheEnable
	server.HttpStrmEnable = requested.HttpStrmEnable
	server.DisableTranscode = requested.DisableTranscode
	server.ResolveStrmLinks = requested.ResolveStrmLinks
	server.Enabled = requested.Enabled

	mediaserver.GetManager().ReloadServer(server.ID)
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
	id := c.Param("id")

	var server models.MediaServer
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "媒体服务器未找到"})
		return
	}

	var req mediaServerUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}

	if req.Name != nil {
		server.Name = *req.Name
	}
	if req.ServerType != nil {
		server.ServerType = *req.ServerType
	}
	if req.ServerAddr != nil {
		server.ServerAddr = *req.ServerAddr
	}
	if req.APIKey != nil && *req.APIKey != "" {
		server.APIKey = *req.APIKey
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
	}

	if ok, msg := validateMediaServer(&server); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": msg})
		return
	}

	if err := database.DB.Save(&server).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "更新媒体服务器失败: " + err.Error()})
		return
	}

	mediaserver.GetManager().ReloadServer(server.ID)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": sanitizeMediaServer(server)})
}

func DeleteMediaServerHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "无效的媒体服务器ID"})
		return
	}
	serverID := uint(id)

	if err := database.DB.Unscoped().Delete(&models.MediaServer{}, serverID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "删除媒体服务器失败: " + err.Error()})
		return
	}

	mediaserver.GetManager().ReloadServer(serverID)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "媒体服务器已删除"})
}

func TestMediaServerConnectionHandler(c *gin.Context) {
	var server models.MediaServer
	if err := c.ShouldBindJSON(&server); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}
	if server.ID != 0 && server.APIKey == "" {
		var stored models.MediaServer
		if err := database.DB.First(&stored, server.ID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "媒体服务器未找到"})
			return
		}
		server.APIKey = stored.APIKey
	}

	if ok, msg := validateMediaServer(&server); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": msg})
		return
	}

	if err := mediaserver.TestConnection(&server); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 1, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "连接成功"})
}

func MediaServerProxyHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "无效的媒体服务器ID"})
		return
	}
	serverID := uint(id)

	mediaserver.GetManager().HandleProxy(c, serverID)
}
