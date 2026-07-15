package handlers

import (
	"cloudstream/internal/models"
	"cloudstream/internal/openlist"
	"cloudstream/internal/pan123"
	"cloudstream/internal/webdav"
	"github.com/gin-gonic/gin"
	"net/http"
)

func TestAccountConnectionHandler(c *gin.Context) {
	var req accountCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "账户凭证无效"})
		return
	}
	account := req.Account

	normalizeAccountType(&account)
	if account.Type == models.AccountTypeOpenList {
		normalizeOpenListAuthMode(&account)
	}
	if ok, msg := applyPlaybackMode(&account, req.PlaybackMode, models.DefaultPlaybackMode(account.Type)); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": msg})
		return
	}

	normalizeAccountCredentials(&account)
	if ok, msg := validateAccount(&account); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": msg})
		return
	}

	testAccount := account
	testAccount.ID = 0
	testAccount.CacheTTL = 0
	testAccount.CustomCachePolicies = ""

	switch account.Type {
	case models.AccountTypeOpenList:
		client := openlist.NewClient(testAccount)
		if err := client.TestConnectionContext(c.Request.Context()); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"code": 1, "message": "OpenList 连接失败"})
			return
		}
	case models.AccountTypeWebDAV:
		client := webdav.NewClient(testAccount)
		if err := client.TestConnectionContext(c.Request.Context()); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"code": 1, "message": "WebDAV 连接失败"})
			return
		}
	default: // 123 云盘开放平台
		client := pan123.NewClient(testAccount)
		if _, err := client.GetAccessTokenForTestContext(c.Request.Context()); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"code": 1, "message": "123 云盘连接失败"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "连接成功！"})
}

func normalizeInactiveOpenListCredentials(account *models.Account) {
	if account.Type != models.AccountTypeOpenList {
		return
	}
	if normalizeOpenListAuthMode(account) == "token" {
		account.OpenListUsername = ""
		account.OpenListPassword = ""
		return
	}
	account.OpenListToken = ""
}
