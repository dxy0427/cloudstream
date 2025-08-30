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

// FileBrowserHandler：浏览pan123云盘文件，按账户拉取未删除文件
func FileBrowserHandler(c *gin.Context) {
	// 获取并校验accountId（必传，非数字无效）
	accountIdStr := c.Query("accountId")
	if accountIdStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "请求必须包含 accountId"})
		return
	}
	accountId, err := strconv.ParseUint(accountIdStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "无效的账户ID"})
		return
	}

	// 查关联的云账户，不存在返回404
	var account models.Account
	if err := database.DB.First(&account, uint(accountId)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "找不到指定的云账户"})
		return
	}

	// 解析父文件夹ID（默认根目录），分页100条/页
	parentFileId, _ := strconv.ParseInt(c.Query("parentFileId"), 10, 64)
	limit := 100

	// 初始化pan123客户端
	client := pan123.NewClient(account)

	var allFiles []pan123.FileInfo
	var lastFileId int64 = 0
	// 分页拉取文件，直到无更多数据
	for {
		files, nextLastFileId, err := client.ListFiles(parentFileId, limit, lastFileId)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": fmt.Sprintf("获取文件列表失败: %s", err.Error())})
			return
		}

		// 过滤回收站文件（Trashed=0）
		for _, file := range files {
			if file.Trashed == 0 {
				allFiles = append(allFiles, file)
			}
		}

		// 无更多文件，退出循环
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
