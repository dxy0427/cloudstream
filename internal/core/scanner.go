package core

import (
	"cloudstream/internal/auth"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/pan123"
	"context"
	"fmt"
	"github.com/rs/zerolog/log"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FileTracker 仅用于本次扫描的内存去重和记录
type FileTracker struct {
	sync.Map
}

func (t *FileTracker) Add(path string) {
	t.Store(path, true)
}

func (t *FileTracker) Has(path string) bool {
	_, ok := t.Load(path)
	return ok
}

func RunScanTask(ctx context.Context, task models.Task) {
	defer func() {
		taskMutex.Lock()
		delete(runningTasks, task.ID)
		taskMutex.Unlock()
		log.Info().Str("task", task.Name).Msg("任务控制权已释放")
	}()

	var account models.Account
	if err := database.DB.First(&account, task.AccountID).Error; err != nil {
		log.Error().Err(err).Str("task", task.Name).Uint("accountID", task.AccountID).Msg("任务启动失败：找不到关联的云账户")
		return
	}

	threads := task.Threads
	if threads < 1 { threads = 1 }
	if threads > 8 { threads = 8 }

	log.Info().Str("task", task.Name).Str("account", account.Name).Int("threads", threads).Msg("开始执行任务")
	client := pan123.NewClient(account)
	strmExtMap := parseExtensions(task.StrmExtensions)
	metaExtMap := parseExtensions(task.MetaExtensions)
	
	tracker := &FileTracker{}

	var wg sync.WaitGroup
	workerPool := make(chan struct{}, threads)
	rateLimiter := time.NewTicker(time.Second / time.Duration(threads))
	defer rateLimiter.Stop()

	startFolderID := task.SourceFolderID
	if account.Type == models.AccountTypeOpenList && (startFolderID == "0" || startFolderID == "") {
		startFolderID = "/"
	}

	// 1. 执行扫描
	scanDirectoryRecursive(ctx, client, task, account.Type, startFolderID, "", task.LocalPath, strmExtMap, metaExtMap, &wg, workerPool, rateLimiter, tracker)

	wg.Wait()

	select {
	case <-ctx.Done():
		log.Warn().Str("task", task.Name).Msg("任务已被手动停止，跳过清理步骤")
	default:
		// 2. 无论是否开启 SyncDelete，都要更新数据库记录 (为了防止未来开启 SyncDelete 时误删)
		updateFileRecords(task.ID, tracker)

		// 3. 如果开启了同步删除，执行清理
		if task.SyncDelete {
			performSafeSyncDelete(task.ID, tracker)
		}
		log.Info().Str("task", task.Name).Msg("任务执行完毕")
	}
}

// updateFileRecords 将本次扫描生成的文件记录到数据库
func updateFileRecords(taskID uint, tracker *FileTracker) {
	// 这是一个简单的实现：遍历 tracker，确保数据库里有记录
	// 性能优化：实际场景中可能需要批量插入，这里为了代码简单使用逐条检查
	tracker.Range(func(key, value interface{}) bool {
		filePath := key.(string)
		var count int64
		database.DB.Model(&models.TaskFile{}).Where("task_id = ? AND file_path = ?", taskID, filePath).Count(&count)
		if count == 0 {
			database.DB.Create(&models.TaskFile{
				TaskID:   taskID,
				FilePath: filePath,
			})
		}
		return true
	})
}

// performSafeSyncDelete 基于数据库记录进行安全删除
func performSafeSyncDelete(taskID uint, currentScanTracker *FileTracker) {
	log.Info().Uint("taskID", taskID).Msg("开始执行基于数据库的安全清理...")
	
	// 1. 获取数据库中该任务记录的所有历史文件
	var historyFiles []models.TaskFile
	if err := database.DB.Where("task_id = ?", taskID).Find(&historyFiles).Error; err != nil {
		log.Error().Err(err).Msg("获取历史文件记录失败，跳过清理")
		return
	}

	deletedCount := 0
	for _, record := range historyFiles {
		// 2. 如果历史记录的文件，在本次扫描中不存在 (currentScanTracker 中没有)
		if !currentScanTracker.Has(record.FilePath) {
			// 说明云端已经删除了这个文件，或者文件改名/移动了
			
			// 删除本地文件
			if err := os.Remove(record.FilePath); err == nil || os.IsNotExist(err) {
				log.Info().Str("file", record.FilePath).Msg("同步删除本地失效文件")
				deletedCount++
			} else {
				log.Warn().Err(err).Str("file", record.FilePath).Msg("删除文件失败")
			}

			// 删除数据库记录
			database.DB.Delete(&record)
		}
	}

	if deletedCount > 0 {
		log.Info().Int("count", deletedCount).Msg("清理完成")
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

	// 记录文件归属
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