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
	MainScheduler *gocron.Scheduler       // 定时任务调度器实例（UTC时区）
	runningTasks  = make(map[uint]context.CancelFunc) // 运行中任务的取消函数（key：任务ID）
	taskMutex     sync.Mutex              // 任务状态互斥锁（防止并发修改runningTasks）
)

// InitScheduler：初始化定时任务调度器（创建实例、加载任务、启动异步运行）
func InitScheduler() {
	MainScheduler = gocron.NewScheduler(time.UTC)
	log.Info().Msg("定时任务调度器已初始化")
	// 加载已启用的任务到调度器
	RefreshScheduler()
	// 异步启动调度器（非阻塞）
	MainScheduler.StartAsync()
	log.Info().Msg("调度器已启动")
}

// RefreshScheduler：刷新调度器（停止旧任务、清空调度、重新加载已启用任务）
func RefreshScheduler() {
	// 1. 加锁停止所有运行中任务，避免并发冲突
	taskMutex.Lock()
	for id, cancel := range runningTasks {
		cancel()         // 触发任务上下文取消
		delete(runningTasks, id) // 移除任务状态
	}
	taskMutex.Unlock()

	// 2. 清空调度器中所有现有任务
	MainScheduler.Clear()

	// 3. 从数据库查询所有已启用的任务
	var tasks []models.Task
	if err := database.DB.Where("enabled = ?", true).Find(&tasks).Error; err != nil {
		log.Error().Err(err).Msg("从数据库加载任务失败")
		return
	}

	log.Info().Int("count", len(tasks)).Msg("发现已启用的任务，正在添加到调度器...")
	// 4. 遍历任务，按Cron表达式添加到调度器
	for _, dbTask := range tasks {
		t := dbTask // 闭包中捕获循环变量需重新赋值
		_, err := MainScheduler.Cron(t.Cron).Do(func() {
			// 执行前检查任务是否已在运行，避免重复执行
			taskMutex.Lock()
			_, exists := runningTasks[t.ID]
			taskMutex.Unlock()
			if exists {
				log.Warn().Str("task", t.Name).Msg("任务已在运行，跳过此次定时执行")
				return
			}

			// 创建任务上下文（用于手动停止），记录到runningTasks
			ctx, cancel := context.WithCancel(context.Background())
			taskMutex.Lock()
			runningTasks[t.ID] = cancel
			taskMutex.Unlock()

			// 执行扫描任务
			RunScanTask(ctx, t)
		})
		if err != nil {
			log.Error().Err(err).Str("task", t.Name).Msg("添加任务到调度器失败")
		}
	}
}

// RunManualTask：手动触发任务执行（检查未运行则启动，返回执行状态）
func RunManualTask(task models.Task) bool {
	// 加锁检查任务是否已在运行（防止并发启动）
	taskMutex.Lock()
	defer taskMutex.Unlock()
	if _, exists := runningTasks[task.ID]; exists {
		return false // 已运行，返回失败
	}

	// 创建取消上下文，启动任务协程
	ctx, cancel := context.WithCancel(context.Background())
	runningTasks[task.ID] = cancel
	go RunScanTask(ctx, task)
	return true // 启动成功
}

// StopTask：停止指定运行中的任务（触发任务上下文取消）
func StopTask(taskID uint) {
	taskMutex.Lock()
	defer taskMutex.Unlock()
	// 若任务存在，触发取消函数（任务内部会处理退出逻辑）
	if cancel, exists := runningTasks[taskID]; exists {
		cancel()
	}
}

// IsTaskRunning：判断任务是否正在运行（检查runningTasks中是否存在任务ID）
func IsTaskRunning(taskID uint) bool {
	taskMutex.Lock()
	defer taskMutex.Unlock()
	_, exists := runningTasks[taskID]
	return exists
}