package handlers

import (
	"cloudstream/internal/core"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

// ListAccountsHandler：处理账户列表查询请求（按ID倒序返回所有账户）
func ListAccountsHandler(c *gin.Context) {
	var accounts []models.Account
	// 按ID倒序查询所有账户
	database.DB.Order("id desc").Find(&accounts)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": accounts})
}

// CreateAccountHandler：处理账户创建请求（接收JSON参数，完成账户新增）
func CreateAccountHandler(c *gin.Context) {
	var account models.Account
	// 绑定请求JSON到账户模型（参数校验）
	if err := c.ShouldBindJSON(&account); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}
	// 数据库新增账户，失败返回错误
	if err := database.DB.Create(&account).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "创建账户失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": account})
}

// UpdateAccountHandler：处理账户更新请求（根据路径ID定位账户，接收JSON参数完成更新）
func UpdateAccountHandler(c *gin.Context) {
	id := c.Param("id")
	var account models.Account
	// 根据路径参数ID查询待更新账户（不存在则返回404）
	if err := database.DB.First(&account, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "账户未找到"})
		return
	}
	// 绑定请求JSON中的更新数据到账户模型
	if err := c.ShouldBindJSON(&account); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}
	// 保存账户更新到数据库
	if err := database.DB.Save(&account).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "更新账户失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": account})
}

// DeleteAccountHandler：处理账户删除请求（含关联任务清理、调度器刷新）
func DeleteAccountHandler(c *gin.Context) {
	idStr := c.Param("id")
	// 校验并转换账户ID（确保为有效数字）
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "无效的账户ID"})
		return
	}
	accountID := uint(id)

	// 查询该账户关联的所有任务
	var tasksUsingAccount []models.Task
	database.DB.Where("account_id = ?", accountID).Find(&tasksUsingAccount)
	// 停止并硬删除所有关联任务
	for _, task := range tasksUsingAccount {
		core.StopTask(task.ID)       // 停止关联的任务
		database.DB.Unscoped().Delete(&task) // 硬删除任务（不保留软删除标记）
	}

	// 硬删除账户本身
	database.DB.Unscoped().Delete(&models.Account{}, accountID)
	// 刷新任务调度器（同步移除已删除任务的调度）
	core.RefreshScheduler()

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "账户及关联任务已删除"})
}