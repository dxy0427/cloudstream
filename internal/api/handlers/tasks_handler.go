package handlers

import (
	"cloudstream/internal/core"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func validateCron(spec string) error {
	_, err := core.ParseCronSpec(spec)
	return err
}

func validateTask(tx *gorm.DB, task *models.Task) error {
	if strings.TrimSpace(task.Name) == "" {
		return fmt.Errorf("任务名称不能为空")
	}
	if task.AccountID == 0 {
		return fmt.Errorf("所属账户不能为空")
	}
	var account models.Account
	if err := tx.First(&account, task.AccountID).Error; err != nil {
		return fmt.Errorf("所属账户不存在")
	}
	if strings.TrimSpace(task.SourceFolderID) == "" {
		return fmt.Errorf("源文件夹不能为空")
	}
	if strings.TrimSpace(task.LocalPath) == "" {
		return fmt.Errorf("本地路径不能为空")
	}
	cleanPath := filepath.Clean(task.LocalPath)
	if !filepath.IsAbs(cleanPath) {
		return fmt.Errorf("本地路径必须是绝对路径")
	}
	if cleanPath == string(filepath.Separator) {
		return fmt.Errorf("本地路径不能是根目录")
	}
	task.LocalPath = cleanPath
	if task.SignExpireHours < 0 {
		return fmt.Errorf("直链有效期不能为负数")
	}
	if task.Threads < 1 || task.Threads > 16 {
		return fmt.Errorf("并发线程必须在 1 到 16 之间")
	}
	return validateCron(task.Cron)
}

func taskNameExists(tx *gorm.DB, name string, excludeID uint) (bool, error) {
	query := tx.Model(&models.Task{}).Where("name = ?", name)
	if excludeID != 0 {
		query = query.Where("id <> ?", excludeID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func buildTaskList() ([]gin.H, error) {
	var tasks []models.Task
	if err := database.DB.Order("id desc").Find(&tasks).Error; err != nil {
		return nil, err
	}
	result := make([]gin.H, 0, len(tasks))
	for _, task := range tasks {
		result = append(result, gin.H{
			"ID":              task.ID,
			"CreatedAt":       task.CreatedAt,
			"UpdatedAt":       task.UpdatedAt,
			"Name":            task.Name,
			"AccountID":       task.AccountID,
			"SourceFolderID":  task.SourceFolderID,
			"LocalPath":       task.LocalPath,
			"Cron":            task.Cron,
			"Enabled":         task.Enabled,
			"Overwrite":       task.Overwrite,
			"SyncDelete":      task.SyncDelete,
			"EncodePath":      task.EncodePath,
			"SignExpireHours": task.SignExpireHours,
			"StrmExtensions":  task.StrmExtensions,
			"MetaExtensions":  task.MetaExtensions,
			"Threads":         task.Threads,
			"ProcessedCount":  task.ProcessedCount,
			"LastRunStatus":   task.LastRunStatus,
			"IsRunning":       core.IsTaskRunning(task.ID),
		})
	}
	return result, nil
}

func ListTasksHandler(c *gin.Context) {
	tasks, err := buildTaskList()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "获取任务列表失败"})
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

	var lastPayload string
	sendSnapshot := func() {
		tasks, err := buildTaskList()
		if err != nil {
			return
		}
		payload, err := json.Marshal(tasks)
		if err != nil || string(payload) == lastPayload {
			return
		}
		lastPayload = string(payload)
		c.SSEvent("tasks", tasks)
		flusher.Flush()
	}

	sendSnapshot()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			sendSnapshot()
		}
	}
}

type taskCreateRequest struct {
	Name            string `json:"Name"`
	AccountID       uint   `json:"AccountID"`
	SourceFolderID  string `json:"SourceFolderID"`
	LocalPath       string `json:"LocalPath"`
	Cron            string `json:"Cron"`
	Enabled         *bool  `json:"Enabled"`
	Overwrite       bool   `json:"Overwrite"`
	SyncDelete      bool   `json:"SyncDelete"`
	EncodePath      bool   `json:"EncodePath"`
	SignExpireHours int    `json:"SignExpireHours"`
	StrmExtensions  string `json:"StrmExtensions"`
	MetaExtensions  string `json:"MetaExtensions"`
	Threads         int    `json:"Threads"`
}

func CreateTaskHandler(c *gin.Context) {
	var req taskCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "参数错误"})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if req.Threads == 0 {
		req.Threads = 4
	}
	if strings.TrimSpace(req.StrmExtensions) == "" {
		req.StrmExtensions = "mp4,mkv,ts,iso"
	}
	if strings.TrimSpace(req.MetaExtensions) == "" {
		req.MetaExtensions = "jpg,jpeg,png,webp,srt,ass,sub"
	}
	task := models.Task{
		Name: req.Name, AccountID: req.AccountID, SourceFolderID: req.SourceFolderID,
		LocalPath: req.LocalPath, Cron: req.Cron, Enabled: enabled, Overwrite: req.Overwrite,
		SyncDelete: req.SyncDelete, EncodePath: req.EncodePath, SignExpireHours: req.SignExpireHours,
		StrmExtensions: req.StrmExtensions, MetaExtensions: req.MetaExtensions, Threads: req.Threads,
	}

	accountTaskMutationMu.Lock()
	defer accountTaskMutationMu.Unlock()

	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := validateTask(tx, &task); err != nil {
			return newAPIMutationError(http.StatusBadRequest, err.Error())
		}
		exists, err := taskNameExists(tx, task.Name, 0)
		if err != nil {
			return err
		}
		if exists {
			return newAPIMutationError(http.StatusConflict, "任务名称已存在")
		}
		if err := tx.Create(&task).Error; err != nil {
			return err
		}
		if !enabled {
			result := tx.Model(&models.Task{}).Where("id = ?", task.ID).Updates(map[string]any{"enabled": false})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return newAPIMutationError(http.StatusNotFound, "找不到指定的任务")
			}
		}
		return nil
	}); err != nil {
		respondMutationError(c, err, "创建任务失败", "任务名称已存在")
		return
	}
	task.Enabled = enabled
	refreshErr := core.RefreshScheduler()
	c.JSON(http.StatusOK, schedulerWarningResponse("任务创建成功", task, refreshErr))
}

type taskUpdateRequest struct {
	Name            *string `json:"Name"`
	AccountID       *uint   `json:"AccountID"`
	SourceFolderID  *string `json:"SourceFolderID"`
	LocalPath       *string `json:"LocalPath"`
	Cron            *string `json:"Cron"`
	Enabled         *bool   `json:"Enabled"`
	Overwrite       *bool   `json:"Overwrite"`
	SyncDelete      *bool   `json:"SyncDelete"`
	EncodePath      *bool   `json:"EncodePath"`
	SignExpireHours *int    `json:"SignExpireHours"`
	StrmExtensions  *string `json:"StrmExtensions"`
	MetaExtensions  *string `json:"MetaExtensions"`
	Threads         *int    `json:"Threads"`
}

func UpdateTaskHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "无效的任务ID"})
		return
	}

	var req taskUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "参数错误"})
		return
	}

	taskID := uint(id)
	accountTaskMutationMu.Lock()
	defer accountTaskMutationMu.Unlock()
	if !core.BlockTaskIfIdle(taskID) {
		c.JSON(http.StatusConflict, gin.H{"code": 1, "message": "任务运行中，停止后才能修改"})
		return
	}
	defer core.UnblockTasks([]uint{taskID})

	var task models.Task
	err = database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&task, taskID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newAPIMutationError(http.StatusNotFound, "找不到指定的任务")
			}
			return err
		}

		originalLocalPath := task.LocalPath
		if req.Name != nil {
			task.Name = *req.Name
		}
		if req.AccountID != nil {
			task.AccountID = *req.AccountID
		}
		if req.SourceFolderID != nil {
			task.SourceFolderID = *req.SourceFolderID
		}
		if req.LocalPath != nil {
			task.LocalPath = *req.LocalPath
		}
		if req.Cron != nil {
			task.Cron = *req.Cron
		}
		if req.Enabled != nil {
			task.Enabled = *req.Enabled
		}
		if req.Overwrite != nil {
			task.Overwrite = *req.Overwrite
		}
		if req.SyncDelete != nil {
			task.SyncDelete = *req.SyncDelete
		}
		if req.EncodePath != nil {
			task.EncodePath = *req.EncodePath
		}
		if req.SignExpireHours != nil {
			task.SignExpireHours = *req.SignExpireHours
		}
		if req.StrmExtensions != nil {
			task.StrmExtensions = *req.StrmExtensions
		}
		if req.MetaExtensions != nil {
			task.MetaExtensions = *req.MetaExtensions
		}
		if req.Threads != nil {
			task.Threads = *req.Threads
		}

		if err := validateTask(tx, &task); err != nil {
			return newAPIMutationError(http.StatusBadRequest, err.Error())
		}
		exists, err := taskNameExists(tx, task.Name, taskID)
		if err != nil {
			return err
		}
		if exists {
			return newAPIMutationError(http.StatusConflict, "任务名称已存在")
		}

		result := tx.Model(&models.Task{}).Where("id = ?", taskID).Updates(map[string]any{
			"name":              task.Name,
			"account_id":        task.AccountID,
			"source_folder_id":  task.SourceFolderID,
			"local_path":        task.LocalPath,
			"cron":              task.Cron,
			"enabled":           task.Enabled,
			"overwrite":         task.Overwrite,
			"sync_delete":       task.SyncDelete,
			"encode_path":       task.EncodePath,
			"sign_expire_hours": task.SignExpireHours,
			"strm_extensions":   task.StrmExtensions,
			"meta_extensions":   task.MetaExtensions,
			"threads":           task.Threads,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return newAPIMutationError(http.StatusNotFound, "找不到指定的任务")
		}
		if task.LocalPath != originalLocalPath {
			if err := tx.Unscoped().Where("task_id = ?", taskID).Delete(&models.TaskFile{}).Error; err != nil {
				return err
			}
		}
		return tx.First(&task, taskID).Error
	})
	if err != nil {
		respondMutationError(c, err, "更新任务失败", "任务名称已存在")
		return
	}
	refreshErr := core.RefreshScheduler()
	c.JSON(http.StatusOK, schedulerWarningResponse("任务更新成功", task, refreshErr))
}

func DeleteTaskHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "无效的任务ID"})
		return
	}
	taskID := uint(id)

	accountTaskMutationMu.Lock()
	defer accountTaskMutationMu.Unlock()

	var task models.Task
	if err := database.DB.First(&task, taskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "找不到指定的任务"})
			return
		}
		respondMutationError(c, err, "查询任务失败", "任务名称已存在")
		return
	}
	if !core.StopTasksAndWait([]uint{taskID}, 30*time.Second) {
		core.UnblockTasks([]uint{taskID})
		c.JSON(http.StatusConflict, gin.H{"code": 1, "message": "任务停止超时，未执行删除"})
		return
	}
	defer core.UnblockTasks([]uint{taskID})
	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("task_id = ?", taskID).Delete(&models.TaskFile{}).Error; err != nil {
			return err
		}
		result := tx.Unscoped().Where("id = ?", taskID).Delete(&models.Task{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return newAPIMutationError(http.StatusNotFound, "找不到指定的任务")
		}
		return nil
	}); err != nil {
		respondMutationError(c, err, "删除任务失败", "任务名称已存在")
		return
	}
	refreshErr := core.RefreshScheduler()
	c.JSON(http.StatusOK, schedulerWarningResponse("任务及关联记录已删除", nil, refreshErr))
}

func ExecuteTaskHandler(c *gin.Context) {
	idValue, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "无效的任务ID"})
		return
	}
	taskID := uint(idValue)

	accountTaskMutationMu.Lock()
	defer accountTaskMutationMu.Unlock()

	var task models.Task
	if err := database.DB.First(&task, taskID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "找不到指定的任务"})
		return
	}
	var account models.Account
	if err := database.DB.First(&account, task.AccountID).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": 1, "message": "任务所属账户不存在"})
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
	taskID := uint(id)

	accountTaskMutationMu.Lock()
	defer accountTaskMutationMu.Unlock()

	var task models.Task
	if err := database.DB.First(&task, taskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "找不到指定的任务"})
			return
		}
		respondMutationError(c, err, "查询任务失败", "任务名称已存在")
		return
	}
	if !core.StopTask(taskID) {
		c.JSON(http.StatusConflict, gin.H{"code": 1, "message": "任务当前未运行"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": fmt.Sprintf("已发送停止信号给任务 #%d。", id)})
}
