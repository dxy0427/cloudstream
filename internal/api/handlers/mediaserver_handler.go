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

func CreateMediaServerHandler(c *gin.Context) {
	var server models.MediaServer
	if err := c.ShouldBindJSON(&server); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}

	if server.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "服务器名称不能为空"})
		return
	}
	if server.ServerType == "" {
		server.ServerType = "Emby"
	}
	if server.ServerAddr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "服务器地址不能为空"})
		return
	}
	if server.APIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "API Key不能为空"})
		return
	}

	if server.PathMappings == "" {
		server.PathMappings = "[]"
	}
	if server.ClientList == "" {
		server.ClientList = "[]"
	}

	if err := database.DB.Create(&server).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "创建媒体服务器失败: " + err.Error()})
		return
	}

	mediaserver.GetManager().ReloadServer(server.ID)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": server})
}

func UpdateMediaServerHandler(c *gin.Context) {
	id := c.Param("id")

	var server models.MediaServer
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "媒体服务器未找到"})
		return
	}

	if err := c.ShouldBindJSON(&server); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}

	if server.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "服务器名称不能为空"})
		return
	}
	if server.ServerType == "" {
		server.ServerType = "Emby"
	}
	if server.ServerAddr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "服务器地址不能为空"})
		return
	}
	if server.APIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "API Key不能为空"})
		return
	}

	if server.PathMappings == "" {
		server.PathMappings = "[]"
	}
	if server.ClientList == "" {
		server.ClientList = "[]"
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
