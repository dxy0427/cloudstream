package handlers

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/pan123"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

// FileBrowserHandler：处理云盘文件浏览请求（按账户拉取文件列表，过滤未删除文件并分页返回）
func FileBrowserHandler(c *gin.Context) {
	// 从查询参数获取账户ID（必传参数，缺失或非数字均返回错误）
	accountIdStr := c.Query("accountId")
	if accountIdStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "请求必须包含 accountId"})
		return
	}
	// 校验并转换账户ID（非数字则返回400，确保后续数据库查询参数有效）
	accountId, err := strconv.ParseUint(accountIdStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "无效的账户ID"})
		return
	}

	// 根据账户ID查询关联的云账户（账户不存在则返回404）
	var account models.Account
	if err := database.DB.First(&account, uint(accountId)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "找不到指定的云账户"})
		return
	}

	// 解析父文件夹ID（非必传参数，忽略转换错误，默认值为0<根目录>），设置分页拉取上限（100条/页）
	parentFileId, _ := strconv.ParseInt(c.Query("parentFileId"), 10, 64)
	limit := 100

	// 初始化pan123客户端（使用查询到的云账户信息建立连接）
	client := pan123.NewClient(account)

	var allFiles []pan123.FileInfo
	var lastFileId int64 = 0
	// 分页拉取文件列表：循环调用接口直到获取所有数据（nextLastFileId=-1表示无更多文件）
	for {
		files, nextLastFileId, err := client.ListFiles(parentFileId, limit, lastFileId)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": fmt.Sprintf("获取文件列表失败: %s", err.Error())})
			return
		}

		// 过滤未删除文件（仅保留回收站外的文件，Trashed=0表示未删除）
		for _, file := range files {
			if file.Trashed == 0 {
				allFiles = append(allFiles, file)
			}
		}

		// 无更多文件时终止循环
		if nextLastFileId == -1 {
			break
		}
		lastFileId = nextLastFileId
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data": gin.H{
			"fileList":   allFiles,
			"lastFileId": -1,
		},
	})
}