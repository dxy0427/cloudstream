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

// StreamHandler：生成云文件播放链接
func StreamHandler(c *gin.Context) {
	// 从URL参数获取accountID和fileID
	accountIDStr := c.Param("accountID")
	fileIDStr := c.Param("fileID")

	// 校验并转换accountID，无效返回400
	accountID, err := strconv.ParseUint(accountIDStr, 10, 32)
	if err != nil {
		c.String(http.StatusBadRequest, "无效的账户 ID")
		return
	}

	// 查询关联的云账户，不存在返回404
	var account models.Account
	if err := database.DB.First(&account, uint(accountID)).Error; err != nil {
		c.String(http.StatusNotFound, "找不到播放链接关联的账户")
		return
	}

	// 校验并转换fileID，无效返回400
	fileID, err := strconv.ParseInt(fileIDStr, 10, 64)
	if err != nil {
		c.String(http.StatusBadRequest, "无效的文件 ID")
		return
	}

	// 用账户凭证获取文件下载链接
	client := pan123.NewClient(account)
	downloadURL, err := client.GetDownloadURL(fileID)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("获取下载链接失败: %v", err))
		return
	}

	// 重定向到下载链接（作为播放地址）
	c.Redirect(http.StatusFound, downloadURL)
}
