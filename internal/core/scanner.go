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
	"sort" // 新增：用于排序目录深度
	"strconv"
	"strings"
	"sync"
	"time"
)

// 高性能 FileTracker (Mutex + Map)
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

// 发送通知（从 notifier.go 整合，确保函数可调用）
func SendNotification(title, content string) {
	var user models.User
	// 获取第一个用户（管理员）的配置
	if err := database.DB.First(&user).Error; err != nil {
		return
	}
	if user.WebhookURL == "" {
		return
	}

	// 构造通用的 JSON 格式 (适配大多数 Webhook，如 Bark, 企业微信等)
	payload := map[string]string{
		"title":   title,
		"body":    content,
		"content": content, // 兼容部分服务
		"msg":     content, // 兼容部分服务
	}
	data, _ := json.Marshal(payload)

	go func() {
		resp, err := http.Post(user.WebhookURL, "application/json", bytes.NewBuffer(data))
		if err != nil {
			log.Error().Err(err).Msg("发送通知失败")
			return
		}
		defer resp.Body.Close()
	}()
}

// 读取最后 N 字节的日志（从 notifier.go 整合，确保函数可调用）
func ReadRecentLogs() (string, error) {
	logPath := "./data/cloudstream.log"
	file, err := os.Open(logPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return "", err
	}

	fileSize := stat.Size()
	readSize := int64(20480) // 读取最后 20KB
	if fileSize < readSize {
		readSize = fileSize
	}

	offset := fileSize - readSize
	buffer := make([]byte, readSize)

	_, err = file.ReadAt(buffer, offset)
	if err != nil && err != io.EOF {
		return "", err
	}

	return string(buffer), nil
}

var taskMutex sync.Mutex
var runningTasks = make(map[uint]struct{})

func RunScanTask(ctx context.Context, task models.Task) {
	taskMutex.Lock()
	runningTasks[task.ID] = struct{}{}
	taskMutex.Unlock()

	defer func() {
		taskMutex.Lock()
		delete(runningTasks, task.ID)
		taskMutex.Unlock()
		log.Info().Str("task", task.Name).Msg("任务控制权已释放")
	}()

	var account models.Account
	if err := database.DB.First(&account, task.AccountID).Error; err != nil {
		log.Error().Err(err).Str("task", task.Name).Uint("accountID", task.AccountID).Msg("任务启动失败：找不到关联的云账户")
		// 任务启动失败通知
		SendNotification("任务启动失败", fmt.Sprintf("任务 '%s' 启动失败：找不到关联的云账户", task.Name))
		return
	}

	threads := task.Threads
	if threads < 1 {
		threads = 1
	}
	if threads > 16 {
		threads = 16
	}

	log.Info().Str("task", task.Name).Str("account", account.Name).Int("threads", threads).Msg("开始执行任务")
	// === 新增：任务开始通知 ===
	SendNotification("任务开始", fmt.Sprintf("任务 '%s' 已开始执行（账户：%s，线程数：%d）", task.Name, account.Name, threads))

	client := pan123.NewClient(account)
	strmExtMap := parseExtensions(task.StrmExtensions)
	metaExtMap := parseExtensions(task.MetaExtensions)

	tracker := NewFileTracker()

	var wg sync.WaitGroup
	workerPool := make(chan struct{}, threads)
	rateLimiter := time.NewTicker(time.Second / time.Duration(threads))
	defer rateLimiter.Stop()

	startFolderID := task.SourceFolderID
	if account.Type == models.AccountTypeOpenList && (startFolderID == "0" || startFolderID == "") {
		startFolderID = "/"
	}

	scanDirectoryRecursive(ctx, client, task, account.Type, startFolderID, "", task.LocalPath, strmExtMap, metaExtMap, &wg, workerPool, rateLimiter, tracker)

	wg.Wait()

	select {
	case <-ctx.Done():
		log.Warn().Str("task", task.Name).Msg("任务已被手动停止，跳过数据库更新和清理")
		// === 新增：任务停止通知 ===
		SendNotification("任务停止", fmt.Sprintf("任务 '%s' 已被手动停止", task.Name))
	default:
		if err := updateFileRecordsOptimized(task.ID, tracker); err != nil {
			log.Error().Err(err).Msg("更新数据库文件记录失败")
			// 数据库更新失败通知
			SendNotification("任务执行异常", fmt.Sprintf("任务 '%s' 执行完毕，但更新数据库文件记录失败", task.Name))
		}

		if task.SyncDelete {
			// 1. 先删文件
			performSafeSyncDeleteOptimized(task.ID, tracker)
			// 2. 后删空目录 (新增)
			cleanEmptyDirs(task.LocalPath)
		}
		log.Info().Str("task", task.Name).Msg("任务执行完毕")
		// === 新增：任务完成通知 ===
		SendNotification("任务完成", fmt.Sprintf("任务 '%s' 已成功执行完毕", task.Name))
	}
}

// === 新增：递归清理空目录逻辑 ===
func cleanEmptyDirs(root string) {
	// 1. 收集所有目录路径
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
		log.Error().Err(err).Msg("遍历目录失败")
		return
	}

	// 2. 按路径长度倒序排序 (确保先处理子目录 A/B/C，再处理 A/B)
	sort.Slice(dirs, func(i, j int) bool {
		return len(dirs[i]) > len(dirs[j])
	})

	// 3. 遍历检查并删除
	removedCount := 0
	for _, d := range dirs {
		// 不删除根目录
		if d == root || d == strings.TrimSuffix(root, "/") {
			continue
		}

		// 读取目录内容
		entries, err := os.ReadDir(d)
		if err == nil && len(entries) == 0 {
			// 如果为空，则删除
			if err := os.Remove(d); err == nil {
				// log.Debug().Str("dir", d).Msg("删除空目录") // 嫌吵可以注释掉
				removedCount++
			}
		}
	}
	if removedCount > 0 {
		log.Info().Int("count", removedCount).Msg("已清理空目录")
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
			records = append(records, models.TaskFile{
				TaskID:   taskID,
				FilePath: p,
			})

			if len(records) >= batchSize || i == len(paths)-1 {
				if err := tx.Clauses(clause.OnConflict{
					DoNothing: true,
				}).CreateInBatches(records, len(records)).Error; err != nil {
					return err
				}
				records = records[:0]
			}
		}
		return nil
	})
}

func performSafeSyncDeleteOptimized(taskID uint, currentScanTracker *FileTracker) {
	log.Info().Uint("taskID", taskID).Msg("开始执行安全清理...")

	deletedCount := 0
	dbDeletedCount := 0
	var lastID uint = 0
	batchSize := 1000

	for {
		var historyFiles []models.TaskFile
		if err := database.DB.Where("task_id = ? AND id > ?", taskID, lastID).
			Order("id asc").Limit(batchSize).Find(&historyFiles).Error; err != nil {
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
					log.Info().Str("file", record.FilePath).Msg("同步删除本地失效文件")
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
		log.Info().Int("deleted_files", deletedCount).Int("db_records_removed", dbDeletedCount).Msg("清理完成")
	}
}

func scanDirectoryRecursive(ctx context.Context, client *pan123.Client, task models.Task, accountType, folderID, currentCloudPath, localBasePath string, strmExtMap, metaExtMap map[string]bool, wg *sync.WaitGroup, pool chan struct{}, limiter *time.Ticker, tracker *FileTracker) {
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
			log.Error().Err(err).Str("task", task.Name).Str("folderID", folderID).Msg("无效的目录ID")
			return
		}
	}

	var lastFileId int64 = 0
	for {
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
					log.Error().Err(err).Str("task", task.Name).Msg("扫描目录失败（123云盘）")
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
				log.Error().Err(err).Str("task", task.Name).Str("path", folderID).Msg("扫描目录失败（OpenList）")
				return
			}
			allFiles = append(allFiles, files...)
			break
		}
	}

	for _, item := range allFiles {
		currentItem := item
		itemCloudPath := path.Join(currentCloudPath, currentItem.FileName)
		nextLocalPath := filepath.Join(localBasePath, currentItem.FileName)

		select {
		case <-ctx.Done():
			return
		default:
		}

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
				scanDirectoryRecursive(ctx, client, task, accountType, nextFolderID, itemCloudPath, nextLocalPath, strmExtMap, metaExtMap, wg, pool, limiter, tracker)
			}()
		} else {
			wg.Add(1)
			go func(fileToProcess pan123.FileInfo, cloudRelPath string) {
				defer wg.Done()
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
					createStrmFile(client, task, fileToProcess, cloudRelPath, localBasePath, tracker)
				} else if metaExtMap[ext] {
					var downloadIdentity interface{}
					if accountType == models.AccountTypeOpenList {
						downloadIdentity = joinOpenListPath(folderID, fileToProcess.FileName)
					} else {
						downloadIdentity = fileToProcess.FileId
					}
					downloadAndSaveMetaFile(client, task, downloadIdentity, fileToProcess.FileName, localBasePath, tracker)
				}
			}(currentItem, itemCloudPath)
		}
	}
}

func createStrmFile(client *pan123.Client, task models.Task, file pan123.FileInfo, cloudRelPath string, localBasePath string, tracker *FileTracker) {
	fileNameWithoutExt := strings.TrimSuffix(file.FileName, filepath.Ext(file.FileName))
	strmFileName := fileNameWithoutExt + ".strm"
	localFilePath := filepath.Join(localBasePath, strmFileName)

	tracker.Add(localFilePath)

	if !task.Overwrite {
		if _, err := os.Stat(localFilePath); err == nil {
			return
		}
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
		sign, err := auth.SignStreamURL(task.AccountID, realIdentity)
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
		log.Info().Str("file", strmFileName).Msg("已生成 STRM 文件")
	}
}

func downloadAndSaveMetaFile(client *pan123.Client, task models.Task, identity interface{}, fileName string, localBasePath string, tracker *FileTracker) {
	localFilePath := filepath.Join(localBasePath, fileName)

	tracker.Add(localFilePath)

	if !task.Overwrite {
		if _, err := os.Stat(localFilePath); err == nil {
			return
		}
	}
	downloadURL, err := client.GetDownloadURL(identity)
	if err != nil {
		log.Error().Err(err).Str("file", fileName).Msg("获取元数据链接失败")
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
		log.Info().Str("file", fileName).Msg("已下载元数据文件")
	}
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