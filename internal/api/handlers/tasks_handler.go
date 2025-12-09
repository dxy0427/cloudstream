package handlers

import (
	"cloudstream/internal/core"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	"net/http"
	"strconv"
)

// 辅助函数：校验 Cron 表达式
func validateCron(spec string) error {
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := parser.Parse(spec)
	if err != nil {
		parserStandard := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		if _, err2 := parserStandard.Parse(spec); err2 != nil {
			return fmt.Errorf("Cron 表达式格式错误")
		}
	}
	return nil
}

func ListTasksHandler(c *gin.Context) {
	var tasks []models.Task
	if err := database.DB.Order("id desc").Find(&tasks).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": fmt.Sprintf("获取任务列表失败: %s", err.Error())})
		return
	}
	type TaskWithStatus struct {
		models.Task
		IsRunning bool `json:"IsRunning"`
	}
	tasksWithStatus := make([]TaskWithStatus, len(tasks))
	for i, task := range tasks {
		tasksWithStatus[i] = TaskWithStatus{
			Task:      task,
			IsRunning: core.IsTaskRunning(task.ID),
		}
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": tasksWithStatus})
}

// ... Create/Update/Delete handlers 保持不变 (它们会自动处理模型变化) ...

func CreateTaskHandler(c *gin.Context) {
	var task models.Task
	if err := c.ShouldBindJSON(&task); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": fmt.Sprintf("参数错误: %s", err.Error())})
		return
	}
	if err := validateCron(task.Cron); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}
	if err := database.DB.Create(&task).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "创建任务失败: " + err.Error()})
		return
	}
	core.RefreshScheduler()
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "任务创建成功", "data": task})
}

func UpdateTaskHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "无效的任务ID"})
		return
	}
	var task models.Task
	if err := database.DB.First(&task, uint(id)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "找不到指定的任务"})
		return
	}
	if err := c.ShouldBindJSON(&task); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": fmt.Sprintf("参数错误: %s", err.Error())})
		return
	}
	if err := validateCron(task.Cron); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}
	if err := database.DB.Save(&task).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "更新任务失败: " + err.Error()})
		return
	}
	core.RefreshScheduler()
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "任务更新成功", "data": task})
}

func DeleteTaskHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "无效的任务ID"})
		return
	}
	taskID := uint(id)
	core.StopTask(taskID)
	if err := database.DB.Unscoped().Delete(&models.Task{}, taskID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": fmt.Sprintf("删除任务失败: %s", err.Error())})
		return
	}
	database.DB.Unscoped().Where("task_id = ?", taskID).Delete(&models.TaskFile{})
	core.RefreshScheduler()
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "任务及关联记录已删除"})
}

func ExecuteTaskHandler(c *gin.Context) {
	id := c.Param("id")
	var task models.Task
	if err := database.DB.First(&task, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "找不到指定的任务"})
		return
	}
	if core.RunManualTask(task) {
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": fmt.Sprintf("任务 '%s' 已开始在后台执行。", task.Name)})
	} else {
		c.JSON(http.StatusConflict, gin.H{"code": 1, "message": fmt.Sprintf("任务 '%s' 已在运行中，请勿重复执行。", task.Name)})
	}
}

func StopTaskHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "无效的任务ID"})
		return
	}
	core.StopTask(uint(id))
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": fmt.Sprintf("已发送停止信号给任务 #%d。", id)})
}