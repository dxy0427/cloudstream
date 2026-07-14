package core

import (
	"cloudstream/internal/auth"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/openlist"
	"cloudstream/internal/pan123"
	"cloudstream/internal/utils"
	"cloudstream/internal/webdav"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type FileTracker struct {
	sync.RWMutex
	files map[string]struct{}
}

type scanOutputRoot struct {
	root    *os.Root
	absPath string
}

var outputTempCounter atomic.Uint64

func openScanOutputRoot(localPath string) (*scanOutputRoot, error) {
	absPath, err := filepath.Abs(localPath)
	if err != nil {
		return nil, fmt.Errorf("解析扫描根目录失败: %w", err)
	}
	absPath = filepath.Clean(absPath)
	if err := os.MkdirAll(absPath, 0755); err != nil {
		return nil, fmt.Errorf("创建扫描根目录失败: %w", err)
	}
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return nil, fmt.Errorf("解析扫描根目录真实路径失败: %w", err)
	}
	resolvedPath, err = filepath.Abs(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("解析扫描根目录真实绝对路径失败: %w", err)
	}
	absPath = filepath.Clean(resolvedPath)
	root, err := os.OpenRoot(absPath)
	if err != nil {
		return nil, fmt.Errorf("打开扫描根目录失败: %w", err)
	}
	return &scanOutputRoot{root: root, absPath: absPath}, nil
}

func cleanRootRelative(name string) (string, error) {
	if name == "" {
		return ".", nil
	}
	cleaned := filepath.Clean(name)
	if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("路径超出扫描根目录: %s", name)
	}
	return cleaned, nil
}

func (r *scanOutputRoot) absolutePath(relativePath string) (string, error) {
	cleaned, err := cleanRootRelative(relativePath)
	if err != nil {
		return "", err
	}
	return filepath.Join(r.absPath, cleaned), nil
}

func (r *scanOutputRoot) relativeFromAbsolute(absolutePath string) (string, bool) {
	if !filepath.IsAbs(absolutePath) {
		return "", false
	}
	relativePath, err := filepath.Rel(r.absPath, filepath.Clean(absolutePath))
	if err != nil {
		return "", false
	}
	cleaned, err := cleanRootRelative(relativePath)
	if err != nil || cleaned == "." {
		return "", false
	}
	return cleaned, true
}

func (r *scanOutputRoot) fileExists(relativePath string) (bool, error) {
	cleaned, err := cleanRootRelative(relativePath)
	if err != nil {
		return false, err
	}
	if _, err := r.root.Stat(cleaned); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *scanOutputRoot) replaceFile(ctx context.Context, relativePath string, perm os.FileMode, write func(io.Writer) error) error {
	cleaned, err := cleanRootRelative(relativePath)
	if err != nil {
		return err
	}
	if cleaned == "." {
		return fmt.Errorf("输出路径不能是扫描根目录")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Dir(cleaned)
	if dir != "." {
		if err := r.root.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("创建目录失败: %w", err)
		}
	}

	var (
		outFile *os.File
		tmpPath string
	)
	for range 100 {
		tmpName := fmt.Sprintf(".cloudstream-%d-%d.tmp", os.Getpid(), outputTempCounter.Add(1))
		tmpPath = filepath.Join(dir, tmpName)
		outFile, err = r.root.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("创建临时文件失败: %w", err)
		}
		break
	}
	if outFile == nil {
		return fmt.Errorf("创建临时文件失败: 临时文件名冲突")
	}
	defer func() {
		_ = outFile.Close()
		_ = r.root.Remove(tmpPath)
	}()

	if err := write(outFile); err != nil {
		return err
	}
	if err := outFile.Sync(); err != nil {
		return fmt.Errorf("同步临时文件失败: %w", err)
	}
	if err := outFile.Close(); err != nil {
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.root.Rename(tmpPath, cleaned); err != nil {
		return fmt.Errorf("原子替换文件失败: %w", err)
	}
	return nil
}

func NewFileTracker() *FileTracker {
	return &FileTracker{files: make(map[string]struct{})}
}

func (t *FileTracker) Add(path string) {
	t.Lock()
	t.files[path] = struct{}{}
	t.Unlock()
}

func (t *FileTracker) Has(path string) bool {
	t.RLock()
	_, ok := t.files[path]
	t.RUnlock()
	return ok
}

func (t *FileTracker) Keys() []string {
	t.RLock()
	defer t.RUnlock()
	keys := make([]string, 0, len(t.files))
	for k := range t.files {
		keys = append(keys, k)
	}
	return keys
}

func (t *FileTracker) Count() int {
	t.RLock()
	defer t.RUnlock()
	return len(t.files)
}

type RunMode string

const (
	RunModeScheduled RunMode = "scheduled"
	RunModeManual    RunMode = "manual"
)

type ScanStats struct {
	newStrmCount atomic.Int64
	newMetaCount atomic.Int64
}

type ScanSummary struct {
	NewStrmCount   int
	NewMetaCount   int
	DeletedCount   int
	TotalStrmCount int
	TotalMetaCount int
}

func (s ScanSummary) HasChanges() bool {
	return s.NewStrmCount > 0 || s.NewMetaCount > 0 || s.DeletedCount > 0
}

func (s ScanSummary) TotalCount() int {
	return s.TotalStrmCount + s.TotalMetaCount
}

func (s ScanSummary) NotificationBody(taskName string) string {
	changeText := fmt.Sprintf("新增 STRM：%d\n新增元数据：%d\n删除文件：%d", s.NewStrmCount, s.NewMetaCount, s.DeletedCount)
	if !s.HasChanges() {
		changeText = "本次无新增或删除"
	}
	return fmt.Sprintf("任务：%s\n%s\nSTRM 总量：%d\n元数据总量：%d", taskName, changeText, s.TotalStrmCount, s.TotalMetaCount)
}

func RunScanTask(ctx context.Context, task models.Task, mode RunMode) {
	database.DB.Model(&models.Task{}).Where("id = ?", task.ID).Updates(map[string]interface{}{
		"last_run_status": "扫描中...",
		"processed_count": 0,
	})

	defer func() {
		finishTaskRun(task.ID)
		log.Info().Str("任务", task.Name).Msg("任务控制权已释放")
	}()

	var account models.Account
	if err := database.DB.First(&account, task.AccountID).Error; err != nil {
		log.Error().Err(err).Str("任务", task.Name).Uint("accountID", task.AccountID).Msg("任务启动失败：找不到关联的云账户")
		updateTaskStatus(task.ID, "失败: 账户丢失", 0)
		return
	}

	threads := task.Threads
	if threads < 1 {
		threads = 1
	}
	if threads > 16 {
		threads = 16
	}

	log.Info().Str("任务", task.Name).Str("账户", account.Name).Int("线程数", threads).Str("运行模式", string(mode)).Msg("开始执行任务")
	outputRoot, err := openScanOutputRoot(task.LocalPath)
	if err != nil {
		log.Error().Err(err).Str("任务", task.Name).Msg("初始化本地扫描根目录失败")
		updateTaskStatus(task.ID, "失败: 本地路径不可用", 0)
		return
	}
	defer outputRoot.root.Close()

	client := pan123.NewClient(account)
	var openListClient *openlist.Client
	var webDavClient *webdav.Client
	if account.Type == models.AccountTypeOpenList {
		openListClient = openlist.NewClient(account)
	} else if account.Type == models.AccountTypeWebDAV {
		webDavClient = webdav.NewClient(account)
	}
	strmExtMap := parseExtensions(task.StrmExtensions)
	metaExtMap := parseExtensions(task.MetaExtensions)

	tracker := NewFileTracker()
	stats := &ScanStats{}
	var hasError atomic.Bool
	hasError.Store(false)

	var wg sync.WaitGroup
	workerPool := make(chan struct{}, threads)
	rateLimiter := time.NewTicker(time.Second / time.Duration(threads))
	defer rateLimiter.Stop()

	progressTicker := time.NewTicker(2 * time.Second)
	progressCtx, cancelProgress := context.WithCancel(context.Background())
	go func() {
		for {
			select {
			case <-progressTicker.C:
				count := tracker.Count()
				database.DB.Model(&models.Task{}).Where("id = ?", task.ID).Update("processed_count", count)
			case <-progressCtx.Done():
				return
			}
		}
	}()

	startFolderID := task.SourceFolderID
	if (account.Type == models.AccountTypeOpenList || account.Type == models.AccountTypeWebDAV) && (startFolderID == "0" || startFolderID == "") {
		startFolderID = "/"
	}

	scanDirectoryRecursive(ctx, client, openListClient, webDavClient, outputRoot, task, account.Type, startFolderID, "", "", strmExtMap, metaExtMap, &wg, workerPool, rateLimiter, tracker, stats, &hasError)

	wg.Wait()
	progressTicker.Stop()
	cancelProgress()

	select {
	case <-ctx.Done():
		message := fmt.Sprintf("任务 '%s' 已被手动停止", task.Name)
		log.Warn().Str("任务", task.Name).Msg("任务已被手动停止")
		updateTaskStatus(task.ID, "用户手动停止", tracker.Count())
		SendNotificationByEvent("任务停止", message, NotifyEventStop)
	default:
		if hasError.Load() {
			message := fmt.Sprintf("任务 '%s' 执行过程中出现错误，为防止误删，已跳过数据库更新和本地清理。", task.Name)
			log.Error().Msg(message)
			updateTaskStatus(task.ID, "异常中止", tracker.Count())
			SendNotificationByEvent("任务异常", message, NotifyEventError)
			return
		}
		if err := ctx.Err(); err != nil {
			updateTaskStatus(task.ID, "用户手动停止", tracker.Count())
			return
		}

		if err := updateFileRecordsOptimized(task.ID, tracker); err != nil {
			message := "更新数据库文件记录失败"
			log.Error().Err(err).Msg(message)
			updateTaskStatus(task.ID, "更新DB失败", tracker.Count())
		} else {
			if err := ctx.Err(); err != nil {
				updateTaskStatus(task.ID, "用户手动停止", tracker.Count())
				return
			}
			deletedCount := 0
			if task.SyncDelete {
				var err error
				deletedCount, err = performSafeSyncDeleteOptimized(ctx, task.ID, outputRoot, tracker)
				if err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						updateTaskStatus(task.ID, "用户手动停止", tracker.Count())
						return
					}
					message := "同步删除未完全成功，失败文件已保留历史记录供下次重试"
					log.Error().Err(err).Str("任务", task.Name).Msg(message)
					updateTaskStatus(task.ID, "同步删除失败", tracker.Count())
					SendNotificationByEvent("任务异常", message, NotifyEventError)
					return
				}
				if err := ctx.Err(); err != nil {
					updateTaskStatus(task.ID, "用户手动停止", tracker.Count())
					return
				}
				cleanEmptyDirs(ctx, outputRoot)
			}
			if err := ctx.Err(); err != nil {
				updateTaskStatus(task.ID, "用户手动停止", tracker.Count())
				return
			}

			totalStrmCount, totalMetaCount := countTrackedOutputs(tracker)
			summary := ScanSummary{
				NewStrmCount:   int(stats.newStrmCount.Load()),
				NewMetaCount:   int(stats.newMetaCount.Load()),
				DeletedCount:   deletedCount,
				TotalStrmCount: totalStrmCount,
				TotalMetaCount: totalMetaCount,
			}

			log.Info().Str("任务", task.Name).Int("总文件", summary.TotalCount()).Int("新增STRM", summary.NewStrmCount).Int("新增元数据", summary.NewMetaCount).Int("删除文件", summary.DeletedCount).Msg("任务执行完毕")
			updateTaskStatus(task.ID, "已完成", tracker.Count())

			message := summary.NotificationBody(task.Name)
			if mode == RunModeManual {
				SendNotificationByEvent("任务完成", message, NotifyEventManual)
			} else if summary.HasChanges() {
				SendNotificationByEvent("任务完成", message, NotifyEventComplete)
			} else {
				log.Info().Str("任务", task.Name).Msg("定时任务本次无变化，跳过发送完成通知")
			}
		}
	}
}

func updateTaskStatus(id uint, status string, count int) {
	database.DB.Model(&models.Task{}).Where("id = ?", id).Updates(map[string]interface{}{
		"last_run_status": status,
		"processed_count": count,
	})
}

func cleanEmptyDirs(ctx context.Context, outputRoot *scanOutputRoot) {
	var dirs []string
	err := fs.WalkDir(outputRoot.root.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			dirs = append(dirs, path)
		}
		return ctx.Err()
	})
	if err != nil {
		return
	}

	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })

	removedCount := 0
	for _, d := range dirs {
		if ctx.Err() != nil {
			return
		}
		if d == "." {
			continue
		}
		dir, err := outputRoot.root.Open(d)
		if err != nil {
			continue
		}
		entries, readErr := dir.ReadDir(1)
		_ = dir.Close()
		if errors.Is(readErr, io.EOF) && len(entries) == 0 {
			if err := outputRoot.root.Remove(d); err == nil {
				removedCount++
			}
		}
	}
	if removedCount > 0 {
		log.Info().Int("数量", removedCount).Msg("已清理空目录")
	}
}

func updateFileRecordsOptimized(taskID uint, tracker *FileTracker) error {
	log.Info().Msg("正在更新数据库文件记录...")
	paths := tracker.Keys()
	if len(paths) == 0 {
		return nil
	}

	batchSize := 500
	records := make([]models.TaskFile, 0, batchSize)

	return database.DB.Transaction(func(tx *gorm.DB) error {
		for i, p := range paths {
			records = append(records, models.TaskFile{TaskID: taskID, FilePath: p})
			if len(records) >= batchSize || i == len(paths)-1 {
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(records, len(records)).Error; err != nil {
					return err
				}
				records = records[:0]
			}
		}
		return nil
	})
}

func performSafeSyncDeleteOptimized(ctx context.Context, taskID uint, outputRoot *scanOutputRoot, currentScanTracker *FileTracker) (int, error) {
	log.Info().Uint("taskID", taskID).Msg("开始执行安全清理...")

	deletedCount := 0
	dbDeletedCount := 0
	var lastID uint = 0
	batchSize := 1000
	var firstDeleteErr error

	for {
		if err := ctx.Err(); err != nil {
			return deletedCount, err
		}
		var historyFiles []models.TaskFile
		if err := database.DB.Where("task_id = ? AND id > ?", taskID, lastID).Order("id asc").Limit(batchSize).Find(&historyFiles).Error; err != nil {
			return deletedCount, fmt.Errorf("查询历史记录失败: %w", err)
		}
		if len(historyFiles) == 0 {
			break
		}

		idsToDelete := make([]uint, 0)
		for _, record := range historyFiles {
			if err := ctx.Err(); err != nil {
				return deletedCount, err
			}
			lastID = record.ID
			if !currentScanTracker.Has(record.FilePath) {
				relativePath, insideRoot := outputRoot.relativeFromAbsolute(record.FilePath)
				if !insideRoot {
					log.Warn().Str("文件", record.FilePath).Msg("历史记录不在当前扫描根目录内，仅移除数据库记录")
					idsToDelete = append(idsToDelete, record.ID)
					continue
				}
				if err := outputRoot.root.Remove(relativePath); err == nil {
					log.Info().Str("文件", record.FilePath).Msg("同步删除本地失效文件")
					deletedCount++
					idsToDelete = append(idsToDelete, record.ID)
				} else if errors.Is(err, os.ErrNotExist) {
					idsToDelete = append(idsToDelete, record.ID)
				} else {
					log.Error().Err(err).Str("文件", record.FilePath).Msg("同步删除失败，保留历史记录")
					if firstDeleteErr == nil {
						firstDeleteErr = err
					}
				}
			}
		}

		if len(idsToDelete) > 0 {
			if err := database.DB.Delete(&models.TaskFile{}, idsToDelete).Error; err != nil {
				return deletedCount, fmt.Errorf("删除历史记录失败: %w", err)
			}
			dbDeletedCount += len(idsToDelete)
		}
	}

	if deletedCount > 0 {
		log.Info().Int("删除文件数", deletedCount).Int("删除记录数", dbDeletedCount).Msg("清理完成")
	}
	return deletedCount, firstDeleteErr
}

func scanDirectoryRecursive(ctx context.Context, client *pan123.Client, openListClient *openlist.Client, webDavClient *webdav.Client, outputRoot *scanOutputRoot, task models.Task, accountType, folderID, currentCloudPath, localRelativePath string, strmExtMap, metaExtMap map[string]bool, wg *sync.WaitGroup, pool chan struct{}, limiter *time.Ticker, tracker *FileTracker, stats *ScanStats, hasError *atomic.Bool) {
	if hasError.Load() {
		return
	}
	select {
	case <-ctx.Done():
		return
	default:
	}

	var (
		folderIDInt int64
		err         error
		allFiles    []pan123.FileInfo
	)

	if accountType == models.AccountType123Pan {
		folderIDInt, err = strconv.ParseInt(folderID, 10, 64)
		if err != nil {
			log.Error().Err(err).Str("任务", task.Name).Str("目录ID", folderID).Msg("无效的目录ID")
			hasError.Store(true)
			return
		}
	}

	var lastFileId int64 = 0
	for {
		if hasError.Load() {
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
		select {
		case <-ctx.Done():
			return
		case <-limiter.C:
		}

		if accountType == models.AccountType123Pan {
			files, nextLastFileId, err := client.ListFilesContext(ctx, folderIDInt, 100, lastFileId, currentCloudPath)
			if err != nil {
				if strings.Contains(strings.ToLower(err.Error()), "429") {
					if !waitForContext(ctx, 3*time.Second) {
						return
					}
					files, nextLastFileId, err = client.ListFilesContext(ctx, folderIDInt, 100, lastFileId, currentCloudPath)
				}
				if err != nil {
					log.Error().Err(err).Str("任务", task.Name).Msg("扫描目录失败（123云盘）")
					hasError.Store(true)
					return
				}
			}
			for _, file := range files {
				if file.Trashed == 0 {
					allFiles = append(allFiles, file)
				}
			}
			if nextLastFileId == -1 {
				break
			}
			lastFileId = nextLastFileId
		} else if accountType == models.AccountTypeOpenList || accountType == models.AccountTypeWebDAV {
			var files []pan123.FileInfo
			var err error
			if accountType == models.AccountTypeWebDAV && webDavClient != nil {
				wFiles, wErr := webDavClient.ListDirectoryContext(ctx, folderID)
				if wErr != nil {
					err = wErr
				} else {
					for _, f := range wFiles {
						ft := 0
						if f.IsDir {
							ft = 1
						}
						files = append(files, pan123.FileInfo{FileName: f.Name, FileType: ft, Size: f.Size})
					}
				}
			} else if openListClient != nil {
				oFiles, oErr := openListClient.ListDirectoryContext(ctx, folderID, false)
				if oErr != nil {
					err = oErr
				} else {
					for _, f := range oFiles {
						ft := 0
						if f.IsDir {
							ft = 1
						}
						files = append(files, pan123.FileInfo{FileName: f.Name, FileType: ft, Size: f.Size})
					}
				}
			}
			if err != nil {
				log.Error().Err(err).Str("任务", task.Name).Str("路径", folderID).Msg("扫描目录失败")
				hasError.Store(true)
				return
			}
			allFiles = append(allFiles, files...)
			break
		}
	}

	strmTargets := make(map[string]string)
	for _, item := range allFiles {
		if item.IsDir() {
			continue
		}
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(item.FileName), "."))
		if !strmExtMap[ext] {
			continue
		}
		strmName := strings.TrimSuffix(item.FileName, filepath.Ext(item.FileName)) + ".strm"
		key := strings.ToLower(strmName)
		if previous, exists := strmTargets[key]; exists {
			log.Error().Str("任务", task.Name).Str("目录", currentCloudPath).Str("文件1", previous).Str("文件2", item.FileName).Str("目标", strmName).Msg("同目录视频映射到同一 STRM 文件")
			hasError.Store(true)
			return
		}
		strmTargets[key] = item.FileName
	}

	for _, item := range allFiles {
		if hasError.Load() {
			return
		}
		currentItem := item
		if currentItem.FileName == "" || filepath.Base(currentItem.FileName) != currentItem.FileName || currentItem.FileName == "." || currentItem.FileName == ".." {
			log.Error().Str("文件", currentItem.FileName).Str("任务", task.Name).Msg("云端文件名包含非法路径片段")
			hasError.Store(true)
			return
		}
		itemCloudPath := path.Join(currentCloudPath, currentItem.FileName)
		nextLocalRelativePath := filepath.Join(localRelativePath, currentItem.FileName)

		if currentItem.IsDir() {
			var nextFolderID string
			if accountType == models.AccountType123Pan {
				nextFolderID = strconv.FormatInt(currentItem.FileId, 10)
			} else {
				nextFolderID = utils.JoinPath(folderID, currentItem.FileName)
			}

			scanDirectoryRecursive(ctx, client, openListClient, webDavClient, outputRoot, task, accountType, nextFolderID, itemCloudPath, nextLocalRelativePath, strmExtMap, metaExtMap, wg, pool, limiter, tracker, stats, hasError)
		} else {
			select {
			case <-ctx.Done():
				return
			case pool <- struct{}{}:
			}
			wg.Add(1)
			go func(fileToProcess pan123.FileInfo, cloudRelPath string) {
				defer wg.Done()
				defer func() { <-pool }()
				if hasError.Load() {
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-limiter.C:
				}

				ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(fileToProcess.FileName), "."))
				if strmExtMap[ext] {
					if err := createStrmFile(ctx, client, accountType, outputRoot, task, fileToProcess, cloudRelPath, localRelativePath, tracker, stats); err != nil {
						if errors.Is(err, context.Canceled) {
							return
						}
						log.Error().Err(err).Str("文件", fileToProcess.FileName).Msg("生成 STRM 失败")
						hasError.Store(true)
					}
				} else if metaExtMap[ext] {
					var downloadIdentity interface{}
					if accountType == models.AccountTypeOpenList || accountType == models.AccountTypeWebDAV {
						downloadIdentity = utils.JoinPath(folderID, fileToProcess.FileName)
					} else {
						downloadIdentity = fileToProcess.FileId
					}
					if err := downloadAndSaveMetaFile(ctx, client, openListClient, webDavClient, accountType, task, outputRoot, downloadIdentity, fileToProcess.FileName, localRelativePath, tracker, stats); err != nil {
						if errors.Is(err, context.Canceled) {
							return
						}
						log.Error().Err(err).Str("文件", fileToProcess.FileName).Msg("下载元数据失败")
						hasError.Store(true)
					}
				}
			}(currentItem, itemCloudPath)
		}
	}
}

func waitForContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func createStrmFile(ctx context.Context, client *pan123.Client, accountType string, outputRoot *scanOutputRoot, task models.Task, file pan123.FileInfo, cloudRelPath string, localRelativePath string, tracker *FileTracker, stats *ScanStats) error {
	fileNameWithoutExt := strings.TrimSuffix(file.FileName, filepath.Ext(file.FileName))
	strmFileName := fileNameWithoutExt + ".strm"
	localRelativeFilePath := filepath.Join(localRelativePath, strmFileName)
	localFilePath, err := outputRoot.absolutePath(localRelativeFilePath)
	if err != nil {
		return err
	}

	existedBefore, err := outputRoot.fileExists(localRelativeFilePath)
	if err != nil {
		return fmt.Errorf("检查 STRM 文件失败: %w", err)
	}
	if !task.Overwrite && existedBefore {
		tracker.Add(localFilePath)
		return nil
	}

	baseURL := client.Account.StrmBaseURL
	if len(baseURL) > 0 && baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}
	if baseURL == "" {
		baseURL = "http://127.0.0.1:12398"
	}

	var realIdentity string
	if accountType == models.AccountTypeOpenList || accountType == models.AccountTypeWebDAV {
		realIdentity = utils.JoinPath(task.SourceFolderID, cloudRelPath)
	} else {
		realIdentity = strconv.FormatInt(file.FileId, 10)
	}

	var streamURL string
	if task.EncodePath {
		sign, err := auth.SignStreamURL(task.ID, task.AccountID, realIdentity, task.SignExpireHours)
		if err != nil {
			return fmt.Errorf("生成签名失败: %w", err)
		}
		displayPath := cloudRelPath
		if !strings.HasPrefix(displayPath, "/") {
			displayPath = "/" + displayPath
		}
		parts := strings.Split(displayPath, "/")
		encodedParts := make([]string, len(parts))
		for i, p := range parts {
			encodedParts[i] = url.PathEscape(p)
		}
		encodedPath := strings.Join(encodedParts, "/")
		streamURL = fmt.Sprintf("%s/api/v1/stream/s%s?sign=%s", baseURL, encodedPath, sign)
	} else {
		if accountType == models.AccountTypeOpenList || accountType == models.AccountTypeWebDAV {
			realParts := strings.Split(realIdentity, "/")
			encRealParts := make([]string, len(realParts))
			for i, p := range realParts {
				encRealParts[i] = url.PathEscape(p)
			}
			encRealIdentity := strings.Join(encRealParts, "/")
			streamURL = fmt.Sprintf("%s/api/v1/stream/s/%d%s", baseURL, task.AccountID, encRealIdentity)
		} else {
			urlPath := cloudRelPath
			if !strings.HasPrefix(urlPath, "/") {
				urlPath = "/" + urlPath
			}
			parts := strings.Split(urlPath, "/")
			encodedParts := make([]string, len(parts))
			for i, p := range parts {
				encodedParts[i] = url.PathEscape(p)
			}
			encodedPath := strings.Join(encodedParts, "/")
			fileIdStr := strconv.FormatInt(file.FileId, 10)
			streamURL = fmt.Sprintf("%s/api/v1/stream/s/%d/%s%s", baseURL, task.AccountID, fileIdStr, encodedPath)
		}
	}

	if err := outputRoot.replaceFile(ctx, localRelativeFilePath, 0644, func(writer io.Writer) error {
		if _, err := io.WriteString(writer, streamURL); err != nil {
			return fmt.Errorf("写入 STRM 文件失败: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("写入 STRM 文件失败: %w", err)
	}
	tracker.Add(localFilePath)
	if !existedBefore {
		stats.newStrmCount.Add(1)
	}
	log.Info().Str("文件", strmFileName).Msg("已生成 STRM 文件")
	return nil
}

func downloadAndSaveMetaFile(ctx context.Context, client *pan123.Client, openListClient *openlist.Client, webDavClient *webdav.Client, accountType string, task models.Task, outputRoot *scanOutputRoot, identity interface{}, fileName string, localRelativePath string, tracker *FileTracker, stats *ScanStats) error {
	localRelativeFilePath := filepath.Join(localRelativePath, fileName)
	localFilePath, err := outputRoot.absolutePath(localRelativeFilePath)
	if err != nil {
		return err
	}
	existedBefore, err := outputRoot.fileExists(localRelativeFilePath)
	if err != nil {
		return fmt.Errorf("检查元数据文件失败: %w", err)
	}
	if !task.Overwrite && existedBefore {
		tracker.Add(localFilePath)
		return nil
	}
	var downloadURL string
	var resp *http.Response
	if accountType == models.AccountTypeOpenList && openListClient != nil {
		pathStr, ok := identity.(string)
		if !ok {
			return fmt.Errorf("OpenList 元数据路径类型错误")
		}
		downloadURL, err = openListClient.GetRawURLContext(ctx, pathStr)
	} else if accountType == models.AccountTypeWebDAV && webDavClient != nil {
		pathStr, ok := identity.(string)
		if !ok {
			return fmt.Errorf("WebDAV 元数据路径类型错误")
		}
		req, reqErr := webDavClient.NewDownloadRequest(ctx, http.MethodGet, pathStr, nil)
		if reqErr != nil {
			return fmt.Errorf("创建 WebDAV 下载请求失败: %w", reqErr)
		}
		resp, err = webDavClient.MetadataHTTPClient.Do(req)
	} else {
		downloadURL, err = client.GetDownloadURLContext(ctx, identity)
	}
	if err != nil {
		return fmt.Errorf("获取元数据链接失败: %w", err)
	}
	if resp == nil {
		httpClient := &http.Client{Timeout: 30 * time.Second}
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
		if reqErr != nil {
			return fmt.Errorf("创建元数据下载请求失败: %w", reqErr)
		}
		resp, err = httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("下载元数据失败: %w", err)
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载元数据返回非200: %d", resp.StatusCode)
	}
	if err := outputRoot.replaceFile(ctx, localRelativeFilePath, 0644, func(writer io.Writer) error {
		if _, err := io.Copy(writer, resp.Body); err != nil {
			return fmt.Errorf("写入文件失败: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("替换元数据文件失败: %w", err)
	}
	tracker.Add(localFilePath)
	if !existedBefore {
		stats.newMetaCount.Add(1)
	}
	log.Info().Str("文件", fileName).Msg("已下载元数据文件")
	return nil
}

func countTrackedOutputs(tracker *FileTracker) (int, int) {
	strmCount := 0
	metaCount := 0
	for _, p := range tracker.Keys() {
		if strings.EqualFold(filepath.Ext(p), ".strm") {
			strmCount++
		} else {
			metaCount++
		}
	}
	return strmCount, metaCount
}

func parseExtensions(extStr string) map[string]bool {
	extMap := make(map[string]bool)
	parts := strings.Split(extStr, ",")
	for _, part := range parts {
		cleanPart := strings.TrimSpace(strings.ToLower(part))
		if cleanPart != "" {
			extMap[cleanPart] = true
		}
	}
	return extMap
}
