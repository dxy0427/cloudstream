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
	// 修复：按 ID 升序排列
	database.DB.Order("id asc").Find(&accounts)
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

func UpdateAccountHandler(c *gin.Context) {
	id := c.Param("id")

	var account models.Account
	if err := database.DB.First(&account, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "账户未找到"})
		return
	}

	if err := c.ShouldBindJSON(&account); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
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
	database.DB.Where("account_id = ?", accountID).Find(&tasksUsingAccount)

	for _, task := range tasksUsingAccount {
		core.StopTask(task.ID)
		database.DB.Unscoped().Delete(&task)
	}

	database.DB.Unscoped().Delete(&models.Account{}, accountID)
	core.RefreshScheduler()
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "账户及关联任务已删除"})
}