package handlers

import (
	"cloudstream/internal/core"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

// ListTasksHandler：查询所有任务，附加运行状态
func ListTasksHandler(c *gin.Context) {
	var tasks []models.Task
	// 按ID倒序查全量任务，失败返回错误
	if err := database.DB.Order("id desc").Find(&tasks).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": fmt.Sprintf("获取任务列表失败: %s", err.Error())})
		return
	}

	// 结构体：任务+运行状态
	type TaskWithStatus struct {
		models.Task
		IsRunning bool `json:"IsRunning"`
	}
	tasksWithStatus := make([]TaskWithStatus, len(tasks))
	// 补充每个任务的运行状态
	for i, task := range tasks {
		tasksWithStatus[i] = TaskWithStatus{
			Task:      task,
			IsRunning: core.IsTaskRunning(task.ID),
		}
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": tasksWithStatus})
}

// CreateTaskHandler：创建任务，同步刷新调度器
func CreateTaskHandler(c *gin.Context) {
	var task models.Task
	// 解析请求参数，格式错误返回400
	if err := c.ShouldBindJSON(&task); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": fmt.Sprintf("参数错误: %s", err.Error())})
		return
	}
	// 入库创建任务，失败返回错误
	if err := database.DB.Create(&task).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "创建任务失败: " + err.Error()})
		return
	}
	// 刷新调度器，加载新任务
	core.RefreshScheduler()

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "任务创建成功", "data": task})
}

// DeleteTaskHandler：删除任务：先停止运行，再硬删并刷新调度
func DeleteTaskHandler(c *gin.Context) {
	idStr := c.Param("id")
	// 校验并转换任务ID，无效返回400
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "无效的任务ID"})
		return
	}
	taskID := uint(id)

	// 停止运行中的任务
	core.StopTask(taskID)
	// 硬删除任务数据，失败返回错误
	if err := database.DB.Unscoped().Delete(&models.Task{}, taskID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": fmt.Sprintf("删除任务失败: %s", err.Error())})
		return
	}
	// 刷新调度器，移除已删任务
	core.RefreshScheduler()

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "任务删除成功"})
}

// ExecuteTaskHandler：手动执行任务，避免重复运行
func ExecuteTaskHandler(c *gin.Context) {
	id := c.Param("id")
	var task models.Task
	// 按ID查任务，不存在返回404
	if err := database.DB.First(&task, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "找不到指定的任务"})
		return
	}

	// 手动触发任务，已运行则返回冲突
	if core.RunManualTask(task) {
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": fmt.Sprintf("任务 '%s' 已开始在后台执行。", task.Name)})
	} else {
		c.JSON(http.StatusConflict, gin.H{"code": 1, "message": fmt.Sprintf("任务 '%s' 已在运行中，请勿重复执行。", task.Name)})
	}
}

// StopTaskHandler：发送任务停止信号
func StopTaskHandler(c *gin.Context) {
	idStr := c.Param("id")
	// 校验并转换任务ID，无效返回400
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "无效的任务ID"})
		return
	}

	// 发送停止信号给任务
	core.StopTask(uint(id))
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": fmt.Sprintf("已发送停止信号给任务 #%d。", id)})
}