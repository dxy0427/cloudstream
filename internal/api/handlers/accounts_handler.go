package handlers

import (
	"cloudstream/internal/core"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

// ListAccountsHandler：查询所有云账户，按ID倒序返回
func ListAccountsHandler(c *gin.Context) {
	var accounts []models.Account
	// 按ID倒序查全量账户
	database.DB.Order("id desc").Find(&accounts)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": accounts})
}

// CreateAccountHandler：新增云账户，接收JSON参数
func CreateAccountHandler(c *gin.Context) {
	var account models.Account
	// 解析请求JSON参数
	if err := c.ShouldBindJSON(&account); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}
	// 入库新增，失败返回错误
	if err := database.DB.Create(&account).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "创建账户失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": account})
}

// UpdateAccountHandler：更新云账户，按路径ID定位，接收JSON参数
func UpdateAccountHandler(c *gin.Context) {
	id := c.Param("id")
	var account models.Account
	// 按ID查账户，不存在返回404
	if err := database.DB.First(&account, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "账户未找到"})
		return
	}
	// 解析更新参数
	if err := c.ShouldBindJSON(&account); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}
	// 保存更新
	if err := database.DB.Save(&account).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "更新账户失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": account})
}

// DeleteAccountHandler：删除云账户，级联清理关联任务并刷新调度
func DeleteAccountHandler(c *gin.Context) {
	idStr := c.Param("id")
	// 校验账户ID格式
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "无效的账户ID"})
		return
	}
	accountID := uint(id)

	// 查关联任务，先停后删（硬删）
	var tasksUsingAccount []models.Task
	database.DB.Where("account_id = ?", accountID).Find(&tasksUsingAccount)
	for _, task := range tasksUsingAccount {
		core.StopTask(task.ID)       // 停止运行中任务
		database.DB.Unscoped().Delete(&task) // 硬删除任务（非软删）
	}

	// 硬删除账户，刷新调度器
	database.DB.Unscoped().Delete(&models.Account{}, accountID)
	core.RefreshScheduler() // 同步调度状态

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "账户及关联任务已删除"})
}
