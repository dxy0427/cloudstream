package handlers

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/openlist"
	"cloudstream/internal/pan123"
	"cloudstream/internal/webdav"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

func TestAccountConnectionHandler(c *gin.Context) {
	var account models.Account
	if err := c.ShouldBindJSON(&account); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "账户凭证无效"})
		return
	}

	normalizeAccountType(&account)
	if account.Type == models.AccountTypeOpenList {
		normalizeOpenListAuthMode(&account)
	}
	if account.ID != 0 {
		var stored models.Account
		if err := database.DB.First(&stored, account.ID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "账户未找到"})
			return
		}
		if ok, msg := mergeStoredAccountSecrets(&account, stored); !ok {
			c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": msg})
			return
		}
	}

	normalizeInactiveOpenListCredentials(&account)
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

func mergeStoredAccountSecrets(account *models.Account, stored models.Account) (bool, string) {
	switch account.Type {
	case models.AccountType123Pan:
		if strings.TrimSpace(account.ClientSecret) != "" {
			return true, ""
		}
		if stored.Type != models.AccountType123Pan || account.ClientID != stored.ClientID {
			return false, "账户类型或 ClientID 已变更，请重新输入 ClientSecret"
		}
		account.ClientSecret = stored.ClientSecret
	case models.AccountTypeOpenList:
		mode := normalizeOpenListAuthMode(account)
		storedMode := normalizeOpenListAuthMode(&stored)
		bindingMatches := stored.Type == models.AccountTypeOpenList &&
			mode == storedMode && sameAccountURL(account.OpenListURL, stored.OpenListURL)

		if mode == "token" && strings.TrimSpace(account.OpenListToken) == "" {
			if !bindingMatches {
				return false, "OpenList 认证方式或地址已变更，请重新输入 Token"
			}
			account.OpenListToken = stored.OpenListToken
		}
		if mode == "password" && strings.TrimSpace(account.OpenListPassword) == "" {
			if !bindingMatches || strings.TrimSpace(account.OpenListUsername) != strings.TrimSpace(stored.OpenListUsername) {
				return false, "OpenList 认证配置已变更，请重新输入密码"
			}
			account.OpenListPassword = stored.OpenListPassword
		}
	case models.AccountTypeWebDAV:
		if strings.TrimSpace(account.WebDAVPassword) != "" {
			return true, ""
		}
		if stored.Type != models.AccountTypeWebDAV ||
			!sameAccountURL(account.WebDAVURL, stored.WebDAVURL) ||
			strings.TrimSpace(account.WebDAVUsername) != strings.TrimSpace(stored.WebDAVUsername) {
			if stored.WebDAVPassword == "" {
				return true, ""
			}
			return false, "WebDAV 地址或用户名已变更，请重新输入密码"
		}
		account.WebDAVPassword = stored.WebDAVPassword
	}
	return true, ""
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

func sameAccountURL(left, right string) bool {
	return normalizeAccountURL(left) == normalizeAccountURL(right)
}

func normalizeAccountURL(value string) string {
	value = strings.TrimSpace(value)
	if value != "" && !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
		value = "http://" + value
	}
	return strings.TrimRight(value, "/")
}
