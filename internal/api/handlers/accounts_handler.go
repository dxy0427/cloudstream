package handlers

import (
	"cloudstream/internal/core"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

func ListAccountsHandler(c *gin.Context) {
	var accounts []models.Account
	if err := database.DB.Order("id asc").Find(&accounts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "获取账户列表失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": accounts})
}

func normalizeAccountType(a *models.Account) {
	if a.Type == "" {
		a.Type = models.AccountType123Pan
	}
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

	if err := database.DB.Create(&account).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "创建账户失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": account})
}

type accountUpdateRequest struct {
	Name             *string `json:"Name"`
	Type             *string `json:"Type"`
	ClientID         *string `json:"ClientID"`
	ClientSecret     *string `json:"ClientSecret"`
	OpenListURL      *string `json:"OpenListURL"`
	OpenListToken    *string `json:"OpenListToken"`
	OpenListUsername *string `json:"OpenListUsername"`
	OpenListPassword *string `json:"OpenListPassword"`
	WebDAVURL        *string `json:"WebDAVURL"`
	WebDAVUsername   *string `json:"WebDAVUsername"`
	WebDAVPassword   *string `json:"WebDAVPassword"`
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
	if req.ClientSecret != nil {
		account.ClientSecret = *req.ClientSecret
	}
	if req.OpenListURL != nil {
		account.OpenListURL = *req.OpenListURL
	}
	if req.OpenListToken != nil {
		account.OpenListToken = *req.OpenListToken
	}
	if req.OpenListUsername != nil {
		account.OpenListUsername = *req.OpenListUsername
	}
	if req.OpenListPassword != nil {
		account.OpenListPassword = *req.OpenListPassword
	}
	if req.WebDAVURL != nil {
		account.WebDAVURL = *req.WebDAVURL
	}
	if req.WebDAVUsername != nil {
		account.WebDAVUsername = *req.WebDAVUsername
	}
	if req.WebDAVPassword != nil {
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

	if err := database.DB.Save(&account).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "更新账户失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": account})
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

	for _, task := range tasksUsingAccount {
		core.StopTask(task.ID)
		if err := database.DB.Unscoped().Where("task_id = ?", task.ID).Delete(&models.TaskFile{}).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "删除关联任务文件记录失败: " + err.Error()})
			return
		}
		if err := database.DB.Unscoped().Delete(&task).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "删除关联任务失败: " + err.Error()})
			return
		}
	}

	if err := database.DB.Unscoped().Delete(&models.Account{}, accountID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "删除账户失败: " + err.Error()})
		return
	}
	core.RefreshScheduler()
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "账户及关联任务已删除"})
}
