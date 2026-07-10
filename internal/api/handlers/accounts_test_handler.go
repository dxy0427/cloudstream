package handlers

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/openlist"
	"cloudstream/internal/pan123"
	"cloudstream/internal/webdav"
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
	if account.ID != 0 {
		var stored models.Account
		if err := database.DB.First(&stored, account.ID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "账户未找到"})
			return
		}
		mergeStoredAccountSecrets(&account, stored)
	}

	switch account.Type {
	case models.AccountTypeOpenList:
		client := openlist.NewClient(account)
		if err := client.TestConnection(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": fmt.Sprintf("OpenList 连接失败: %s", err.Error())})
			return
		}
	case models.AccountTypeWebDAV:
		client := webdav.NewClient(account)
		if err := client.TestConnection(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": fmt.Sprintf("WebDAV 连接失败: %s", err.Error())})
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

func mergeStoredAccountSecrets(account *models.Account, stored models.Account) {
	if account.Type == models.AccountType123Pan && account.ClientSecret == "" {
		account.ClientSecret = stored.ClientSecret
	}
	if account.Type == models.AccountTypeOpenList {
		if normalizeOpenListAuthMode(account) == "token" && account.OpenListToken == "" {
			account.OpenListToken = stored.OpenListToken
		}
		if account.OpenListAuthMode == "password" && account.OpenListPassword == "" {
			account.OpenListPassword = stored.OpenListPassword
		}
	}
	if account.Type == models.AccountTypeWebDAV && account.WebDAVPassword == "" {
		account.WebDAVPassword = stored.WebDAVPassword
	}
}
