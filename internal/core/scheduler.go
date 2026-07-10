package core

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"context"
	"fmt"
	"github.com/go-co-op/gocron"
	"github.com/rs/zerolog/log"
	"sync"
	"time"
)

var (
	MainScheduler   *gocron.Scheduler
	runningTasks    = make(map[uint]context.CancelFunc)
	runningTaskDone = make(map[uint]chan struct{})
	blockedTasks    = make(map[uint]struct{})
	taskMutex       sync.Mutex
	schedulerMutex  sync.Mutex
)

func InitScheduler() {
	log.Info().Msg("定时任务调度器已初始化")
	if err := RefreshScheduler(); err != nil {
		log.Error().Err(err).Msg("调度器启动失败")
	}
}

func RefreshScheduler() error {
	schedulerMutex.Lock()
	defer schedulerMutex.Unlock()

	var tasks []models.Task
	if err := database.DB.Where("enabled = ?", true).Find(&tasks).Error; err != nil {
		return fmt.Errorf("从数据库加载任务失败: %w", err)
	}
	log.Info().Int("count", len(tasks)).Msg("发现已启用的任务，正在添加到调度器...")

	nextScheduler := gocron.NewScheduler(time.Local)
	for _, dbTask := range tasks {
		t := dbTask
		withSeconds, err := ParseCronSpec(t.Cron)
		if err != nil {
			return fmt.Errorf("任务 %q 的 Cron 无效: %w", t.Name, err)
		}

		var jobScheduler *gocron.Scheduler
		if withSeconds {
			jobScheduler = nextScheduler.CronWithSeconds(t.Cron)
		} else {
			jobScheduler = nextScheduler.Cron(t.Cron)
		}
		_, err = jobScheduler.Do(func() {
			taskMutex.Lock()
			if _, blocked := blockedTasks[t.ID]; blocked {
				taskMutex.Unlock()
				return
			}
			if _, exists := runningTasks[t.ID]; exists {
				taskMutex.Unlock()
				log.Warn().Str("task", t.Name).Msg("任务已在运行，跳过此次定时执行")
				return
			}
			ctx, cancel := context.WithCancel(context.Background())
			runningTasks[t.ID] = cancel
			runningTaskDone[t.ID] = make(chan struct{})
			taskMutex.Unlock()

			RunScanTask(ctx, t, RunModeScheduled)
		})

		if err != nil {
			return fmt.Errorf("添加任务 %q 到调度器失败: %w", t.Name, err)
		}
	}

	oldScheduler := MainScheduler
	if oldScheduler != nil {
		oldScheduler.Clear()
		go oldScheduler.Stop()
	}
	MainScheduler = nextScheduler
	MainScheduler.StartAsync()

	log.Info().Msg("调度器已启动")
	return nil
}

func RunManualTask(task models.Task) bool {
	taskMutex.Lock()
	defer taskMutex.Unlock()
	if _, blocked := blockedTasks[task.ID]; blocked {
		return false
	}
	if _, exists := runningTasks[task.ID]; exists {
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	runningTasks[task.ID] = cancel
	runningTaskDone[task.ID] = make(chan struct{})
	go RunScanTask(ctx, task, RunModeManual)
	return true
}

func StopTask(taskID uint) {
	taskMutex.Lock()
	defer taskMutex.Unlock()
	if cancel, exists := runningTasks[taskID]; exists {
		cancel()
	}
}

func StopTasksAndWait(taskIDs []uint, timeout time.Duration) bool {
	taskMutex.Lock()
	doneChannels := make([]chan struct{}, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		blockedTasks[taskID] = struct{}{}
		if cancel, exists := runningTasks[taskID]; exists {
			cancel()
			if done := runningTaskDone[taskID]; done != nil {
				doneChannels = append(doneChannels, done)
			}
		}
	}
	taskMutex.Unlock()

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for _, done := range doneChannels {
		select {
		case <-done:
		case <-deadline.C:
			return false
		}
	}
	return true
}

func UnblockTasks(taskIDs []uint) {
	taskMutex.Lock()
	defer taskMutex.Unlock()
	for _, taskID := range taskIDs {
		delete(blockedTasks, taskID)
	}
}

func IsTaskRunning(taskID uint) bool {
	taskMutex.Lock()
	defer taskMutex.Unlock()
	_, exists := runningTasks[taskID]
	return exists
}
