package core

import (
	"cloudstream/internal/auth"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/pan123"
	"context"
	"fmt"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"io"
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
)

// 高性能 FileTracker
type FileTracker struct {
	sync.RWMutex
	files map[string]struct{}
}

func NewFileTracker() *FileTracker {
	return &FileTracker{
		files: make(map[string]struct{}),
	}
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
		taskMutex.Lock()
		delete(runningTasks, task.ID)
		taskMutex.Unlock()
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
	client := pan123.NewClient(account)
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
	defer cancelProgress()
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
	if account.Type == models.AccountTypeOpenList && (startFolderID == "0" || startFolderID == "") {
		startFolderID = "/"
	}

	scanDirectoryRecursive(ctx, client, task, account.Type, startFolderID, "", task.LocalPath, strmExtMap, metaExtMap, &wg, workerPool, rateLimiter, tracker, stats, &hasError)

	wg.Wait()
	progressTicker.Stop()

	select {
	case <-ctx.Done():
		log.Warn().Str("任务", task.Name).Msg("任务已被手动停止")
		updateTaskStatus(task.ID, "用户手动停止", tracker.Count())
		SendNotification("任务停止", fmt.Sprintf("任务 '%s' 已被手动停止", task.Name))
	default:
		if hasError.Load() {
			msg := fmt.Sprintf("任务 '%s' 执行过程中出现错误，为防止误删，已跳过数据库更新和本地清理。", task.Name)
			log.Error().Msg(msg)
			updateTaskStatus(task.ID, "异常中止", tracker.Count())
			SendNotification("任务异常", msg)
			return
		}

		if err := updateFileRecordsOptimized(task.ID, tracker); err != nil {
			log.Error().Err(err).Msg("更新数据库文件记录失败")
			updateTaskStatus(task.ID, "更新DB失败", tracker.Count())
		} else {
			deletedCount := 0
			if task.SyncDelete {
				deletedCount = performSafeSyncDeleteOptimized(task.ID, tracker)
				cleanEmptyDirs(task.LocalPath)
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

			if mode == RunModeManual || summary.HasChanges() {
				SendNotification("任务完成", summary.NotificationBody(task.Name))
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

func cleanEmptyDirs(root string) {
	var dirs []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		return
	}

	sort.Slice(dirs, func(i, j int) bool {
		return len(dirs[i]) > len(dirs[j])
	})

	removedCount := 0
	for _, d := range dirs {
		if d == root || d == strings.TrimSuffix(root, "/") {
			continue
		}
		entries, err := os.ReadDir(d)
		if err == nil && len(entries) == 0 {
			if err := os.Remove(d); err == nil {
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

func performSafeSyncDeleteOptimized(taskID uint, currentScanTracker *FileTracker) int {
	log.Info().Uint("taskID", taskID).Msg("开始执行安全清理...")

	deletedCount := 0
	dbDeletedCount := 0
	var lastID uint = 0
	batchSize := 1000

	for {
		var historyFiles []models.TaskFile
		if err := database.DB.Where("task_id = ? AND id > ?", taskID, lastID).Order("id asc").Limit(batchSize).Find(&historyFiles).Error; err != nil {
			log.Error().Err(err).Msg("查询历史记录失败")
			break
		}
		if len(historyFiles) == 0 {
			break
		}

		idsToDelete := make([]uint, 0)
		for _, record := range historyFiles {
			lastID = record.ID
			if !currentScanTracker.Has(record.FilePath) {
				if err := os.Remove(record.FilePath); err == nil || os.IsNotExist(err) {
					log.Info().Str("文件", record.FilePath).Msg("同步删除本地失效文件")
					deletedCount++
				}
				idsToDelete = append(idsToDelete, record.ID)
			}
		}

		if len(idsToDelete) > 0 {
			if err := database.DB.Delete(&models.TaskFile{}, idsToDelete).Error; err == nil {
				dbDeletedCount += len(idsToDelete)
			}
		}
	}

	if deletedCount > 0 {
		log.Info().Int("删除文件数", deletedCount).Int("删除记录数", dbDeletedCount).Msg("清理完成")
	}
	return deletedCount
}

func scanDirectoryRecursive(ctx context.Context, client *pan123.Client, task models.Task, accountType, folderID, currentCloudPath, localBasePath string, strmExtMap, metaExtMap map[string]bool, wg *sync.WaitGroup, pool chan struct{}, limiter *time.Ticker, tracker *FileTracker, stats *ScanStats, hasError *atomic.Bool) {
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
		<-limiter.C

		if accountType == models.AccountType123Pan {
			files, nextLastFileId, err := client.ListFiles(folderIDInt, 100, lastFileId, "")
			if err != nil {
				if strings.Contains(err.Error(), "code: 429") {
					time.Sleep(3 * time.Second)
					files, nextLastFileId, err = client.ListFiles(folderIDInt, 100, lastFileId, "")
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
		} else if accountType == models.AccountTypeOpenList {
			files, err := client.ListOpenListDirectory(folderID)
			if err != nil {
				log.Error().Err(err).Str("任务", task.Name).Str("路径", folderID).Msg("扫描目录失败（OpenList）")
				hasError.Store(true)
				return
			}
			allFiles = append(allFiles, files...)
			break
		}
	}

	for _, item := range allFiles {
		if hasError.Load() {
			return
		}

		currentItem := item
		itemCloudPath := path.Join(currentCloudPath, currentItem.FileName)
		nextLocalPath := filepath.Join(localBasePath, currentItem.FileName)

		if currentItem.IsDir() {
			var nextFolderID string
			if accountType == models.AccountType123Pan {
				nextFolderID = strconv.FormatInt(currentItem.FileId, 10)
			} else {
				nextFolderID = joinOpenListPath(folderID, currentItem.FileName)
			}

			wg.Add(1)
			go func() {
				defer wg.Done()
				select {
				case <-ctx.Done():
					return
				case pool <- struct{}{}:
				}
				defer func() { <-pool }()
				scanDirectoryRecursive(ctx, client, task, accountType, nextFolderID, itemCloudPath, nextLocalPath, strmExtMap, metaExtMap, wg, pool, limiter, tracker, stats, hasError)
			}()
		} else {
			wg.Add(1)
			go func(fileToProcess pan123.FileInfo, cloudRelPath string) {
				defer wg.Done()
				if hasError.Load() {
					return
				}

				select {
				case <-ctx.Done():
					return
				case pool <- struct{}{}:
				}
				defer func() { <-pool }()
				select {
				case <-ctx.Done():
					return
				case <-limiter.C:
				}

				ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(fileToProcess.FileName), "."))
				if strmExtMap[ext] {
					createStrmFile(client, task, fileToProcess, cloudRelPath, localBasePath, tracker, stats)
				} else if metaExtMap[ext] {
					var downloadIdentity interface{}
					if accountType == models.AccountTypeOpenList {
						downloadIdentity = joinOpenListPath(folderID, fileToProcess.FileName)
					} else {
						downloadIdentity = fileToProcess.FileId
					}
					downloadAndSaveMetaFile(client, task, downloadIdentity, fileToProcess.FileName, localBasePath, tracker, stats)
				}
			}(currentItem, itemCloudPath)
		}
	}
}

func createStrmFile(client *pan123.Client, task models.Task, file pan123.FileInfo, cloudRelPath string, localBasePath string, tracker *FileTracker, stats *ScanStats) {
	fileNameWithoutExt := strings.TrimSuffix(file.FileName, filepath.Ext(file.FileName))
	strmFileName := fileNameWithoutExt + ".strm"
	localFilePath := filepath.Join(localBasePath, strmFileName)

	tracker.Add(localFilePath)

	_, statErr := os.Stat(localFilePath)
	existedBefore := statErr == nil
	if !task.Overwrite && existedBefore {
		return
	}

	baseURL := client.Account.StrmBaseURL
	if len(baseURL) > 0 && baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}
	if baseURL == "" {
		baseURL = "http://127.0.0.1:12398"
	}

	var realIdentity string
	if client.Account.Type == models.AccountTypeOpenList {
		realIdentity = joinOpenListPath(task.SourceFolderID, cloudRelPath)
	} else {
		realIdentity = strconv.FormatInt(file.FileId, 10)
	}

	var streamURL string
	if task.EncodePath {
		sign, err := auth.SignStreamURL(task.ID, task.AccountID, realIdentity)
		if err != nil {
			log.Error().Err(err).Msg("生成签名失败")
			return
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
		if client.Account.Type == models.AccountTypeOpenList {
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

	if err := os.MkdirAll(filepath.Dir(localFilePath), 0755); err != nil {
		return
	}

	if err := os.WriteFile(localFilePath, []byte(streamURL), 0644); err == nil {
		if !existedBefore {
			stats.newStrmCount.Add(1)
		}
		log.Info().Str("文件", strmFileName).Msg("已生成 STRM 文件")
	}
}

func downloadAndSaveMetaFile(client *pan123.Client, task models.Task, identity interface{}, fileName string, localBasePath string, tracker *FileTracker, stats *ScanStats) {
	localFilePath := filepath.Join(localBasePath, fileName)
	tracker.Add(localFilePath)

	_, statErr := os.Stat(localFilePath)
	existedBefore := statErr == nil
	if !task.Overwrite && existedBefore {
		return
	}
	downloadURL, err := client.GetDownloadURL(identity)
	if err != nil {
		log.Error().Err(err).Str("文件", fileName).Msg("获取元数据链接失败")
		return
	}
	resp, err := http.Get(downloadURL)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	if err := os.MkdirAll(filepath.Dir(localFilePath), 0755); err != nil {
		return
	}
	outFile, err := os.Create(localFilePath)
	if err != nil {
		return
	}
	defer outFile.Close()
	if _, err := io.Copy(outFile, resp.Body); err == nil {
		if !existedBefore {
			stats.newMetaCount.Add(1)
		}
		log.Info().Str("文件", fileName).Msg("已下载元数据文件")
	}
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

func joinOpenListPath(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || p == "0" {
			continue
		}
		if i == 0 {
			if p == "/" {
				cleaned = append(cleaned, "")
				continue
			}
			p = "/" + strings.TrimLeft(p, "/")
		} else {
			p = strings.Trim(p, "/")
		}
		cleaned = append(cleaned, p)
	}
	result := path.Join(cleaned...)
	if !strings.HasPrefix(result, "/") {
		result = "/" + result
	}
	return result
}
