package handlers

import (
	"cloudstream/internal/models"
	"cloudstream/internal/openlist"
	"cloudstream/internal/pan123"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
)

func TestAccountConnectionHandler(c *gin.Context) {
	var account models.Account
	if err := c.ShouldBindJSON(&account); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "账户凭证无效"})
		return
	}

	if account.Type == "" {
		account.Type = models.AccountType123Pan
	}

	switch account.Type {
	case models.AccountTypeOpenList:
		client := openlist.NewClient(account)
		if err := client.TestConnection(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": fmt.Sprintf("OpenList 连接失败: %s", err.Error())})
			return
		}
	default: // 123 云盘开放平台
		client := pan123.NewClient(account)
		if _, err := client.GetAccessTokenForTest(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": fmt.Sprintf("123 云盘连接失败: %s", err.Error())})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "连接成功！"})
}