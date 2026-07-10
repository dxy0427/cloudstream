package handlers

import (
	"cloudstream/internal/core"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/openlist"
	"cloudstream/internal/pan123"
	"cloudstream/internal/webdav"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
	"strconv"
	"time"
)

func ListAccountsHandler(c *gin.Context) {
	var accounts []models.Account
	if err := database.DB.Order("id asc").Find(&accounts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "获取账户列表失败: " + err.Error()})
		return
	}
	data := make([]gin.H, 0, len(accounts))
	for _, account := range accounts {
		data = append(data, sanitizeAccount(account))
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": data})
}

func sanitizeAccount(account models.Account) gin.H {
	return gin.H{
		"ID": account.ID, "CreatedAt": account.CreatedAt, "UpdatedAt": account.UpdatedAt,
		"Name": account.Name, "Type": account.Type, "ClientID": account.ClientID,
		"OpenListURL": account.OpenListURL, "OpenListAuthMode": normalizeOpenListAuthMode(&account), "OpenListUsername": account.OpenListUsername,
		"WebDAVURL": account.WebDAVURL, "WebDAVUsername": account.WebDAVUsername,
		"StrmBaseURL": account.StrmBaseURL, "CacheTTL": account.CacheTTL,
		"CustomCachePolicies": account.CustomCachePolicies,
		"HasClientSecret":     account.ClientSecret != "", "HasOpenListToken": account.OpenListToken != "",
		"HasOpenListPassword": account.OpenListPassword != "", "HasWebDAVPassword": account.WebDAVPassword != "",
	}
}

func normalizeAccountType(a *models.Account) {
	if a.Type == "" {
		a.Type = models.AccountType123Pan
	}
}

func normalizeOpenListAuthMode(a *models.Account) string {
	if a.OpenListAuthMode != "token" && a.OpenListAuthMode != "password" {
		if a.OpenListToken != "" {
			a.OpenListAuthMode = "token"
		} else {
			a.OpenListAuthMode = "password"
		}
	}
	return a.OpenListAuthMode
}

func validateAccount(a *models.Account) (ok bool, msg string) {
	normalizeAccountType(a)

	switch a.Type {
	case models.AccountType123Pan:
		if a.Name == "" || a.ClientID == "" || a.ClientSecret == "" {
			return false, "123 云盘账户名称、ClientID、ClientSecret 不能为空"
		}
	case models.AccountTypeOpenList:
		if a.Name == "" || a.OpenListURL == "" {
			return false, "OpenList 账户名称和地址不能为空"
		}
		if normalizeOpenListAuthMode(a) == "token" && a.OpenListToken == "" {
			return false, "OpenList Token 不能为空"
		}
		if a.OpenListAuthMode == "password" && (a.OpenListUsername == "" || a.OpenListPassword == "") {
			return false, "OpenList 用户名和密码不能为空"
		}
	case models.AccountTypeWebDAV:
		if a.Name == "" || a.WebDAVURL == "" {
			return false, "WebDAV 账户名称和地址不能为空"
		}
	default:
		return false, "不支持的云账户类型"
	}
	return true, ""
}

func CreateAccountHandler(c *gin.Context) {
	var account models.Account
	if err := c.ShouldBindJSON(&account); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}

	if ok, msg := validateAccount(&account); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": msg})
		return
	}

	requestedCacheTTL := account.CacheTTL
	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&account).Error; err != nil {
			return err
		}
		if requestedCacheTTL == 0 {
			return tx.Model(&account).UpdateColumn("cache_ttl", 0).Error
		}
		return nil
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "创建账户失败: " + err.Error()})
		return
	}
	account.CacheTTL = requestedCacheTTL
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": sanitizeAccount(account)})
}

type accountUpdateRequest struct {
	Name                *string `json:"Name"`
	Type                *string `json:"Type"`
	ClientID            *string `json:"ClientID"`
	ClientSecret        *string `json:"ClientSecret"`
	OpenListURL         *string `json:"OpenListURL"`
	OpenListAuthMode    *string `json:"OpenListAuthMode"`
	OpenListToken       *string `json:"OpenListToken"`
	OpenListUsername    *string `json:"OpenListUsername"`
	OpenListPassword    *string `json:"OpenListPassword"`
	WebDAVURL           *string `json:"WebDAVURL"`
	WebDAVUsername      *string `json:"WebDAVUsername"`
	WebDAVPassword      *string `json:"WebDAVPassword"`
	StrmBaseURL         *string `json:"StrmBaseURL"`
	CacheTTL            *int    `json:"CacheTTL"`
	CustomCachePolicies *string `json:"CustomCachePolicies"`
}

func UpdateAccountHandler(c *gin.Context) {
	id := c.Param("id")

	var account models.Account
	if err := database.DB.First(&account, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "账户未找到"})
		return
	}

	var req accountUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}

	if req.Name != nil {
		account.Name = *req.Name
	}
	if req.Type != nil {
		account.Type = *req.Type
	}
	if req.ClientID != nil {
		account.ClientID = *req.ClientID
	}
	if req.ClientSecret != nil && *req.ClientSecret != "" {
		account.ClientSecret = *req.ClientSecret
	}
	if req.OpenListURL != nil {
		account.OpenListURL = *req.OpenListURL
	}
	if req.OpenListAuthMode != nil {
		account.OpenListAuthMode = *req.OpenListAuthMode
	}
	if req.OpenListToken != nil && *req.OpenListToken != "" {
		account.OpenListToken = *req.OpenListToken
	}
	if req.OpenListUsername != nil {
		account.OpenListUsername = *req.OpenListUsername
	}
	if req.OpenListPassword != nil && *req.OpenListPassword != "" {
		account.OpenListPassword = *req.OpenListPassword
	}
	if req.WebDAVURL != nil {
		account.WebDAVURL = *req.WebDAVURL
	}
	if req.WebDAVUsername != nil {
		account.WebDAVUsername = *req.WebDAVUsername
	}
	if req.WebDAVPassword != nil && *req.WebDAVPassword != "" {
		account.WebDAVPassword = *req.WebDAVPassword
	}
	if req.StrmBaseURL != nil {
		account.StrmBaseURL = *req.StrmBaseURL
	}
	if req.CacheTTL != nil {
		account.CacheTTL = *req.CacheTTL
	}
	if req.CustomCachePolicies != nil {
		account.CustomCachePolicies = *req.CustomCachePolicies
	}

	if ok, msg := validateAccount(&account); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": msg})
		return
	}
	if account.Type == models.AccountTypeOpenList {
		if account.OpenListAuthMode == "token" {
			account.OpenListUsername = ""
			account.OpenListPassword = ""
		} else {
			account.OpenListToken = ""
		}
	}

	if err := database.DB.Save(&account).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "更新账户失败: " + err.Error()})
		return
	}
	InvalidateStreamClient(account.ID)
	openlist.InvalidateAccountCache(account.ID)
	pan123.InvalidateAccountCache(account.ID)
	webdav.InvalidateAccountCache(account.ID)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": sanitizeAccount(account)})
}

func DeleteAccountHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "无效的账户ID"})
		return
	}
	accountID := uint(id)

	var tasksUsingAccount []models.Task
	if err := database.DB.Where("account_id = ?", accountID).Find(&tasksUsingAccount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "查询关联任务失败: " + err.Error()})
		return
	}

	taskIDs := make([]uint, 0, len(tasksUsingAccount))
	for _, task := range tasksUsingAccount {
		taskIDs = append(taskIDs, task.ID)
	}
	if !core.StopTasksAndWait(taskIDs, 30*time.Second) {
		core.UnblockTasks(taskIDs)
		c.JSON(http.StatusConflict, gin.H{"code": 1, "message": "关联任务停止超时，未执行删除"})
		return
	}
	defer core.UnblockTasks(taskIDs)

	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("task_id IN (?)", tx.Model(&models.Task{}).Select("id").Where("account_id = ?", accountID)).Delete(&models.TaskFile{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("account_id = ?", accountID).Delete(&models.Task{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Delete(&models.Account{}, accountID).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "删除账户失败: " + err.Error()})
		return
	}
	InvalidateStreamClient(accountID)
	openlist.InvalidateAccountCache(accountID)
	pan123.InvalidateAccountCache(accountID)
	webdav.InvalidateAccountCache(accountID)
	if err := core.RefreshScheduler(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "账户已删除，但刷新调度器失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "账户及关联任务已删除"})
}
