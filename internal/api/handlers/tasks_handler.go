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
	"time"
)

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

func buildTaskList() ([]gin.H, error) {
	var tasks []models.Task
	if err := database.DB.Order("id desc").Find(&tasks).Error; err != nil {
		return nil, err
	}
	result := make([]gin.H, 0, len(tasks))
	for _, task := range tasks {
		result = append(result, gin.H{
			"ID":             task.ID,
			"CreatedAt":      task.CreatedAt,
			"UpdatedAt":      task.UpdatedAt,
			"Name":           task.Name,
			"AccountID":      task.AccountID,
			"SourceFolderID": task.SourceFolderID,
			"LocalPath":      task.LocalPath,
			"Cron":           task.Cron,
			"Enabled":        task.Enabled,
			"Overwrite":      task.Overwrite,
			"SyncDelete":     task.SyncDelete,
			"EncodePath":     task.EncodePath,
			"StrmExtensions": task.StrmExtensions,
			"MetaExtensions": task.MetaExtensions,
			"Threads":        task.Threads,
			"ProcessedCount": task.ProcessedCount,
			"LastRunStatus":  task.LastRunStatus,
			"IsRunning":      core.IsTaskRunning(task.ID),
		})
	}
	return result, nil
}

func ListTasksHandler(c *gin.Context) {
	tasks, err := buildTaskList()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": fmt.Sprintf("获取任务列表失败: %s", err.Error())})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": tasks})
}

func StreamTasksHandler(c *gin.Context) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "stream not supported"})
		return
	}

	sendSnapshot := func() {
		tasks, err := buildTaskList()
		if err != nil {
			return
		}
		c.SSEvent("tasks", tasks)
		flusher.Flush()
	}

	sendSnapshot()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-time.After(2 * time.Second):
			sendSnapshot()
		}
	}
}

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

type taskUpdateRequest struct {
	Name           *string `json:"Name"`
	AccountID      *uint   `json:"AccountID"`
	SourceFolderID *string `json:"SourceFolderID"`
	LocalPath      *string `json:"LocalPath"`
	Cron           *string `json:"Cron"`
	Enabled        *bool   `json:"Enabled"`
	Overwrite      *bool   `json:"Overwrite"`
	SyncDelete     *bool   `json:"SyncDelete"`
	EncodePath     *bool   `json:"EncodePath"`
	StrmExtensions *string `json:"StrmExtensions"`
	MetaExtensions *string `json:"MetaExtensions"`
	Threads        *int    `json:"Threads"`
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

	var req taskUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": fmt.Sprintf("参数错误: %s", err.Error())})
		return
	}

	if req.Name != nil { task.Name = *req.Name }
	if req.AccountID != nil { task.AccountID = *req.AccountID }
	if req.SourceFolderID != nil { task.SourceFolderID = *req.SourceFolderID }
	if req.LocalPath != nil { task.LocalPath = *req.LocalPath }
	if req.Cron != nil { task.Cron = *req.Cron }
	if req.Enabled != nil { task.Enabled = *req.Enabled }
	if req.Overwrite != nil { task.Overwrite = *req.Overwrite }
	if req.SyncDelete != nil { task.SyncDelete = *req.SyncDelete }
	if req.EncodePath != nil { task.EncodePath = *req.EncodePath }
	if req.StrmExtensions != nil { task.StrmExtensions = *req.StrmExtensions }
	if req.MetaExtensions != nil { task.MetaExtensions = *req.MetaExtensions }
	if req.Threads != nil { task.Threads = *req.Threads }

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
	if err := database.DB.Unscoped().Where("task_id = ?", taskID).Delete(&models.TaskFile{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": fmt.Sprintf("删除任务关联记录失败: %s", err.Error())})
		return
	}
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
