package handlers

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/openlist"
	"cloudstream/internal/pan123"
	"cloudstream/internal/utils"
	"cloudstream/internal/webdav"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"net/http"
	"strconv"
)

type CloudFileDTO struct {
	FileId   string `json:"fileId"`
	FileName string `json:"filename"`
	Type     int    `json:"type"` // 1=目录, 0=文件
}

func FileBrowserHandler(c *gin.Context) {
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

	var account models.Account
	if err := database.DB.First(&account, uint(accountId)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "找不到指定的云账户"})
		return
	}

	parentParam := c.Query("parentFileId")
	var fileList []CloudFileDTO

	switch account.Type {
	case models.AccountTypeOpenList:
		// OpenList 使用 path
		parentPath := parentParam
		if parentPath == "" || parentPath == "0" {
			parentPath = "/"
		}

		client := openlist.NewClient(account)
		items, err := client.ListDirectoryContext(c.Request.Context(), parentPath, false)
		if err != nil {
			log.Error().Err(err).Uint64("accountID", accountId).Msg("获取 OpenList 文件列表失败")
			c.JSON(http.StatusBadGateway, gin.H{"code": 1, "message": "OpenList 服务暂时不可用"})
			return
		}

		for _, item := range items {
			childPath := utils.JoinPath(parentPath, item.Name)
			t := 0
			if item.IsDir {
				t = 1
			}
			fileList = append(fileList, CloudFileDTO{
				FileId:   childPath,
				FileName: item.Name,
				Type:     t,
			})
		}

	case models.AccountTypeWebDAV:
		// WebDAV 使用 path
		parentPath := parentParam
		if parentPath == "" || parentPath == "0" {
			parentPath = "/"
		}

		client := webdav.NewClient(account)
		items, err := client.ListDirectoryContext(c.Request.Context(), parentPath)
		if err != nil {
			log.Error().Err(err).Uint64("accountID", accountId).Msg("获取 WebDAV 文件列表失败")
			c.JSON(http.StatusBadGateway, gin.H{"code": 1, "message": "WebDAV 服务暂时不可用"})
			return
		}

		for _, item := range items {
			childPath := utils.JoinPath(parentPath, item.Name)
			t := 0
			if item.IsDir {
				t = 1
			}
			fileList = append(fileList, CloudFileDTO{
				FileId:   childPath,
				FileName: item.Name,
				Type:     t,
			})
		}

	default: // 123 云盘开放平台
		parentFileId, _ := strconv.ParseInt(parentParam, 10, 64)
		limit := 100
		client := pan123.NewClient(account)

		var lastFileId int64 = 0
		for {
			files, nextLastFileId, err := client.ListFilesContext(c.Request.Context(), parentFileId, limit, lastFileId, "")
			if err != nil {
				log.Error().Err(err).Uint64("accountID", accountId).Msg("获取 123 云盘文件列表失败")
				c.JSON(http.StatusBadGateway, gin.H{"code": 1, "message": "123 云盘服务暂时不可用"})
				return
			}

			for _, file := range files {
				if file.Trashed == 0 {
					fileList = append(fileList, CloudFileDTO{
						FileId:   strconv.FormatInt(file.FileId, 10),
						FileName: file.FileName,
						Type:     file.FileType,
					})
				}
			}

			if nextLastFileId == -1 {
				break
			}
			lastFileId = nextLastFileId
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data": gin.H{
			"fileList":   fileList,
			"lastFileId": -1,
		},
	})
}
