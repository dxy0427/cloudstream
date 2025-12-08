package core

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"context"
	"github.com/go-co-op/gocron"
	"github.com/rs/zerolog/log"
	"sync"
	"time"
)

var (
	MainScheduler *gocron.Scheduler
	runningTasks  = make(map[uint]context.CancelFunc)
	taskMutex     sync.Mutex
)

func InitScheduler() {
	MainScheduler = gocron.NewScheduler(time.UTC)
	log.Info().Msg("定时任务调度器已初始化")
	RefreshScheduler()
	MainScheduler.StartAsync()
	log.Info().Msg("调度器已启动")
}

func RefreshScheduler() {
	taskMutex.Lock()
	for id, cancel := range runningTasks {
		cancel()
		delete(runningTasks, id)
	}
	taskMutex.Unlock()

	MainScheduler.Clear()

	var tasks []models.Task
	if err := database.DB.Where("enabled = ?", true).Find(&tasks).Error; err != nil {
		log.Error().Err(err).Msg("从数据库加载任务失败")
		return
	}
	log.Info().Int("count", len(tasks)).Msg("发现已启用的任务，正在添加到调度器...")

	for _, dbTask := range tasks {
		t := dbTask
		_, err := MainScheduler.Cron(t.Cron).Do(func() {
			taskMutex.Lock()
			if _, exists := runningTasks[t.ID]; exists {
				taskMutex.Unlock()
				log.Warn().Str("task", t.Name).Msg("任务已在运行，跳过此次定时执行")
				return
			}
			ctx, cancel := context.WithCancel(context.Background())
			runningTasks[t.ID] = cancel
			taskMutex.Unlock()

			RunScanTask(ctx, t)
		})

		if err != nil {
			log.Error().Err(err).Str("task", t.Name).Msg("添加任务到调度器失败")
		}
	}
}

func RunManualTask(task models.Task) bool {
	taskMutex.Lock()
	defer taskMutex.Unlock()
	if _, exists := runningTasks[task.ID]; exists {
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	runningTasks[task.ID] = cancel
	go RunScanTask(ctx, task)
	return true
}

func StopTask(taskID uint) {
	taskMutex.Lock()
	defer taskMutex.Unlock()
	if cancel, exists := runningTasks[taskID]; exists {
		cancel()
	}
}

func IsTaskRunning(taskID uint) bool {
	taskMutex.Lock()
	defer taskMutex.Unlock()
	_, exists := runningTasks[taskID]
	return exists
}