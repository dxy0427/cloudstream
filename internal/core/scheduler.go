package core

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-co-op/gocron"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

var (
	MainScheduler   *gocron.Scheduler
	runningTasks    = make(map[uint]context.CancelFunc)
	runningTaskDone = make(map[uint]chan struct{})
	blockedTasks    = make(map[uint]struct{})
	taskMutex       sync.Mutex
	schedulerMutex  sync.Mutex
	schedulerClosed atomic.Bool
)

func InitScheduler() {
	schedulerClosed.Store(false)
	log.Info().Msg("定时任务调度器已初始化")
	if err := RefreshScheduler(); err != nil {
		log.Error().Err(err).Msg("调度器启动失败")
	}
}

func RefreshScheduler() error {
	schedulerMutex.Lock()
	defer schedulerMutex.Unlock()
	if schedulerClosed.Load() {
		return fmt.Errorf("调度器已关闭")
	}

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
			log.Error().Err(err).Uint("taskID", t.ID).Str("task", t.Name).Str("cron", t.Cron).Msg("任务 Cron 无效，已跳过")
			continue
		}

		var jobScheduler *gocron.Scheduler
		if withSeconds {
			jobScheduler = nextScheduler.CronWithSeconds(t.Cron)
		} else {
			jobScheduler = nextScheduler.Cron(t.Cron)
		}
		_, err = jobScheduler.Do(func() {
			ctx, ok := reserveTaskRun(t.ID)
			if !ok {
				if IsTaskRunning(t.ID) {
					log.Warn().Str("task", t.Name).Msg("任务已在运行，跳过此次定时执行")
				}
				return
			}
			handled := false
			defer func() {
				if !handled {
					finishTaskRun(t.ID)
				}
			}()

			var currentTask models.Task
			if err := database.DB.First(&currentTask, t.ID).Error; err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					log.Error().Err(err).Uint("taskID", t.ID).Msg("定时任务执行前校验失败")
				}
				return
			}
			if !currentTask.Enabled || currentTask.Cron != t.Cron {
				return
			}
			var account models.Account
			if err := database.DB.First(&account, currentTask.AccountID).Error; err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					log.Error().Err(err).Uint("taskID", t.ID).Uint("accountID", currentTask.AccountID).Msg("定时任务账户校验失败")
				}
				return
			}

			if ctx.Err() != nil {
				return
			}
			handled = true
			RunScanTask(ctx, currentTask, RunModeScheduled)
		})

		if err != nil {
			log.Error().Err(err).Uint("taskID", t.ID).Str("task", t.Name).Msg("添加任务到调度器失败，已跳过")
			continue
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
	ctx, ok := reserveTaskRun(task.ID)
	if !ok {
		return false
	}
	go RunScanTask(ctx, task, RunModeManual)
	return true
}

func reserveTaskRun(taskID uint) (context.Context, bool) {
	taskMutex.Lock()
	defer taskMutex.Unlock()
	if schedulerClosed.Load() {
		return nil, false
	}
	if _, blocked := blockedTasks[taskID]; blocked {
		return nil, false
	}
	if _, exists := runningTasks[taskID]; exists {
		return nil, false
	}

	ctx, cancel := context.WithCancel(context.Background())
	runningTasks[taskID] = cancel
	runningTaskDone[taskID] = make(chan struct{})
	return ctx, true
}

func StopTask(taskID uint) bool {
	taskMutex.Lock()
	defer taskMutex.Unlock()
	if cancel, exists := runningTasks[taskID]; exists {
		cancel()
		return true
	}
	return false
}

func BlockTaskIfIdle(taskID uint) bool {
	taskMutex.Lock()
	defer taskMutex.Unlock()
	if schedulerClosed.Load() {
		return false
	}
	if _, running := runningTasks[taskID]; running {
		return false
	}
	if _, blocked := blockedTasks[taskID]; blocked {
		return false
	}
	blockedTasks[taskID] = struct{}{}
	return true
}

func finishTaskRun(taskID uint) {
	taskMutex.Lock()
	delete(runningTasks, taskID)
	if done, ok := runningTaskDone[taskID]; ok {
		close(done)
		delete(runningTaskDone, taskID)
	}
	taskMutex.Unlock()
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

func ShutdownScheduler(timeout time.Duration) bool {
	schedulerClosed.Store(true)

	taskMutex.Lock()
	doneChannels := make([]chan struct{}, 0, len(runningTasks))
	for taskID, cancel := range runningTasks {
		blockedTasks[taskID] = struct{}{}
		cancel()
		if done := runningTaskDone[taskID]; done != nil {
			doneChannels = append(doneChannels, done)
		}
	}
	taskMutex.Unlock()

	schedulerMutex.Lock()
	scheduler := MainScheduler
	MainScheduler = nil
	if scheduler != nil {
		scheduler.Clear()
	}
	schedulerMutex.Unlock()

	schedulerDone := make(chan struct{})
	go func() {
		if scheduler != nil {
			scheduler.Stop()
		}
		close(schedulerDone)
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	wait := func(done <-chan struct{}) bool {
		select {
		case <-done:
			return true
		case <-timer.C:
			return false
		}
	}
	if !wait(schedulerDone) {
		return false
	}
	for _, done := range doneChannels {
		if !wait(done) {
			return false
		}
	}
	return true
}
