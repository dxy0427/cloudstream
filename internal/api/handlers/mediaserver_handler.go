package handlers

import (
	"cloudstream/internal/database"
	"cloudstream/internal/mediaserver"
	"cloudstream/internal/models"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

func ListMediaServersHandler(c *gin.Context) {
	var servers []models.MediaServer
	database.DB.Order("id asc").Find(&servers)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": servers})
}

func validateMediaServer(server *models.MediaServer) (bool, string) {
	if server.Name == "" {
		return false, "服务器名称不能为空"
	}
	if server.ServerType == "" {
		server.ServerType = "Emby"
	}
	if server.ServerAddr == "" {
		return false, "服务器地址不能为空"
	}
	if server.APIKey == "" {
		return false, "API Key不能为空"
	}
	if server.PathMappings == "" {
		server.PathMappings = "[]"
	}
	if server.ClientList == "" {
		server.ClientList = "[]"
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

	if err := database.DB.Create(&server).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "创建媒体服务器失败: " + err.Error()})
		return
	}

	mediaserver.GetManager().ReloadServer(server.ID)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": server})
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
	if req.APIKey != nil {
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
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": server})
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

	if ok, msg := validateMediaServer(&server); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": msg})
		return
	}

	if err := mediaserver.TestConnection(&server); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 1, "message": err.Error()})
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
