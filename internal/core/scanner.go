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

type FileTracker struct {
	sync.RWMutex
	files map[string]struct{}
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
	if (account.Type == models.AccountTypeOpenList || account.Type == models.AccountTypeWebDAV) && (startFolderID == "0" || startFolderID == "") {
		startFolderID = "/"
	}

	scanDirectoryRecursive(ctx, client, openListClient, webDavClient, task, account.Type, startFolderID, "", task.LocalPath, strmExtMap, metaExtMap, &wg, workerPool, rateLimiter, tracker, stats, &hasError)

	wg.Wait()
	progressTicker.Stop()

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

		if err := updateFileRecordsOptimized(task.ID, tracker); err != nil {
			message := "更新数据库文件记录失败"
			log.Error().Err(err).Msg(message)
			updateTaskStatus(task.ID, "更新DB失败", tracker.Count())
		} else {
			deletedCount := 0
			if task.SyncDelete {
				var err error
				deletedCount, err = performSafeSyncDeleteOptimized(task.ID, tracker)
				if err != nil {
					message := "同步删除未完全成功，失败文件已保留历史记录供下次重试"
					log.Error().Err(err).Str("任务", task.Name).Msg(message)
					updateTaskStatus(task.ID, "同步删除失败", tracker.Count())
					SendNotificationByEvent("任务异常", message, NotifyEventError)
					return
				}
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

	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })

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

func performSafeSyncDeleteOptimized(taskID uint, currentScanTracker *FileTracker) (int, error) {
	log.Info().Uint("taskID", taskID).Msg("开始执行安全清理...")

	deletedCount := 0
	dbDeletedCount := 0
	var lastID uint = 0
	batchSize := 1000
	var firstDeleteErr error

	for {
		var historyFiles []models.TaskFile
		if err := database.DB.Where("task_id = ? AND id > ?", taskID, lastID).Order("id asc").Limit(batchSize).Find(&historyFiles).Error; err != nil {
			return deletedCount, fmt.Errorf("查询历史记录失败: %w", err)
		}
		if len(historyFiles) == 0 {
			break
		}

		idsToDelete := make([]uint, 0)
		for _, record := range historyFiles {
			lastID = record.ID
			if !currentScanTracker.Has(record.FilePath) {
				if err := os.Remove(record.FilePath); err == nil {
					log.Info().Str("文件", record.FilePath).Msg("同步删除本地失效文件")
					deletedCount++
					idsToDelete = append(idsToDelete, record.ID)
				} else if os.IsNotExist(err) {
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

func scanDirectoryRecursive(ctx context.Context, client *pan123.Client, openListClient *openlist.Client, webDavClient *webdav.Client, task models.Task, accountType, folderID, currentCloudPath, localBasePath string, strmExtMap, metaExtMap map[string]bool, wg *sync.WaitGroup, pool chan struct{}, limiter *time.Ticker, tracker *FileTracker, stats *ScanStats, hasError *atomic.Bool) {
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
		} else if accountType == models.AccountTypeOpenList || accountType == models.AccountTypeWebDAV {
			var files []pan123.FileInfo
			var err error
			if accountType == models.AccountTypeWebDAV && webDavClient != nil {
				wFiles, wErr := webDavClient.ListDirectory(folderID)
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
				oFiles, oErr := openListClient.ListDirectory(folderID, false)
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
		nextLocalPath := filepath.Join(localBasePath, currentItem.FileName)

		if currentItem.IsDir() {
			var nextFolderID string
			if accountType == models.AccountType123Pan {
				nextFolderID = strconv.FormatInt(currentItem.FileId, 10)
			} else {
				nextFolderID = utils.JoinPath(folderID, currentItem.FileName)
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
				scanDirectoryRecursive(ctx, client, openListClient, webDavClient, task, accountType, nextFolderID, itemCloudPath, nextLocalPath, strmExtMap, metaExtMap, wg, pool, limiter, tracker, stats, hasError)
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
					if err := createStrmFile(client, openListClient, webDavClient, accountType, task, fileToProcess, cloudRelPath, localBasePath, tracker, stats); err != nil {
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
					if err := downloadAndSaveMetaFile(client, openListClient, webDavClient, accountType, task, downloadIdentity, fileToProcess.FileName, localBasePath, tracker, stats); err != nil {
						log.Error().Err(err).Str("文件", fileToProcess.FileName).Msg("下载元数据失败")
						hasError.Store(true)
					}
				}
			}(currentItem, itemCloudPath)
		}
	}
}

func createStrmFile(client *pan123.Client, openListClient *openlist.Client, webDavClient *webdav.Client, accountType string, task models.Task, file pan123.FileInfo, cloudRelPath string, localBasePath string, tracker *FileTracker, stats *ScanStats) error {
	fileNameWithoutExt := strings.TrimSuffix(file.FileName, filepath.Ext(file.FileName))
	strmFileName := fileNameWithoutExt + ".strm"
	localFilePath := filepath.Join(localBasePath, strmFileName)

	_, statErr := os.Stat(localFilePath)
	existedBefore := statErr == nil
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

	if err := os.MkdirAll(filepath.Dir(localFilePath), 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	if err := os.WriteFile(localFilePath, []byte(streamURL), 0644); err != nil {
		return fmt.Errorf("写入 STRM 文件失败: %w", err)
	}
	tracker.Add(localFilePath)
	if !existedBefore {
		stats.newStrmCount.Add(1)
	}
	log.Info().Str("文件", strmFileName).Msg("已生成 STRM 文件")
	return nil
}

func downloadAndSaveMetaFile(client *pan123.Client, openListClient *openlist.Client, webDavClient *webdav.Client, accountType string, task models.Task, identity interface{}, fileName string, localBasePath string, tracker *FileTracker, stats *ScanStats) error {
	localFilePath := filepath.Join(localBasePath, fileName)
	_, statErr := os.Stat(localFilePath)
	existedBefore := statErr == nil
	if !task.Overwrite && existedBefore {
		tracker.Add(localFilePath)
		return nil
	}
	var downloadURL string
	var err error
	var resp *http.Response
	if accountType == models.AccountTypeOpenList && openListClient != nil {
		pathStr, ok := identity.(string)
		if !ok {
			return fmt.Errorf("OpenList 元数据路径类型错误")
		}
		downloadURL, err = openListClient.GetRawURL(pathStr)
	} else if accountType == models.AccountTypeWebDAV && webDavClient != nil {
		pathStr, ok := identity.(string)
		if !ok {
			return fmt.Errorf("WebDAV 元数据路径类型错误")
		}
		req, reqErr := webDavClient.NewDownloadRequest(http.MethodGet, pathStr, nil)
		if reqErr != nil {
			return fmt.Errorf("创建 WebDAV 下载请求失败: %w", reqErr)
		}
		resp, err = webDavClient.MetadataHTTPClient.Do(req)
	} else {
		downloadURL, err = client.GetDownloadURL(identity)
	}
	if err != nil {
		return fmt.Errorf("获取元数据链接失败: %w", err)
	}
	if resp == nil {
		httpClient := &http.Client{Timeout: 30 * time.Second}
		resp, err = httpClient.Get(downloadURL)
		if err != nil {
			return fmt.Errorf("下载元数据失败: %w", err)
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载元数据返回非200: %d", resp.StatusCode)
	}
	dir := filepath.Dir(localFilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	outFile, err := os.CreateTemp(dir, "."+filepath.Base(fileName)+".tmp-*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpPath := outFile.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(outFile, resp.Body); err != nil {
		_ = outFile.Close()
		return fmt.Errorf("写入文件失败: %w", err)
	}
	if err := outFile.Close(); err != nil {
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}
	if err := os.Rename(tmpPath, localFilePath); err != nil {
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
