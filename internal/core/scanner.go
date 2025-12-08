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

// 使用 sync.Map 安全地记录已生成的文件
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
	
	// 初始化文件追踪器
	tracker := &FileTracker{}

	var wg sync.WaitGroup
	workerPool := make(chan struct{}, threads)
	rateLimiter := time.NewTicker(time.Second / time.Duration(threads))
	defer rateLimiter.Stop()

	startFolderID := task.SourceFolderID
	if account.Type == models.AccountTypeOpenList && (startFolderID == "0" || startFolderID == "") {
		startFolderID = "/"
	}

	// 开始递归扫描
	scanDirectoryRecursive(ctx, client, task, account.Type, startFolderID, "", task.LocalPath, strmExtMap, metaExtMap, &wg, workerPool, rateLimiter, tracker)

	wg.Wait()

	select {
	case <-ctx.Done():
		log.Warn().Str("task", task.Name).Msg("任务已被手动停止，跳过清理步骤")
	default:
		// 如果开启了同步删除，执行清理
		if task.SyncDelete {
			performSyncDelete(task, tracker, strmExtMap, metaExtMap)
		}
		log.Info().Str("task", task.Name).Msg("任务执行完毕")
	}
}

// performSyncDelete 遍历本地目录，删除多余文件
func performSyncDelete(task models.Task, tracker *FileTracker, strmExts, metaExts map[string]bool) {
	log.Info().Str("task", task.Name).Msg("开始执行本地清理 (Sync Delete)...")
	deletedCount := 0

	err := filepath.Walk(task.LocalPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}

		// 检查扩展名是否在我们的管理范围内
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
		// 如果是 STRM 文件，或者是元数据文件，我们才考虑删除
		if !strmExts[ext] && !metaExts[ext] && ext != "strm" {
			return nil
		}

		// 如果该文件没有在本次扫描中生成/确认，则删除
		if !tracker.Has(path) {
			if err := os.Remove(path); err == nil {
				log.Info().Str("file", path).Msg("删除已失效的本地文件")
				deletedCount++
			} else {
				log.Warn().Err(err).Str("file", path).Msg("删除文件失败")
			}
		}
		return nil
	})

	if err != nil {
		log.Error().Err(err).Msg("清理本地文件时出错")
	} else {
		log.Info().Int("count", deletedCount).Msg("本地清理完成")
	}
	
	// 清理空目录 (可选，简单实现)
	cleanEmptyDirs(task.LocalPath)
}

func cleanEmptyDirs(root string) {
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if info.IsDir() && path != root {
			// 尝试删除，如果非空会失败，忽略错误即可
			os.Remove(path)
		}
		return nil
	})
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

	// === 核心修改：记录此文件路径，证明它在云端存在 ===
	tracker.Add(localFilePath)
	// ==================================================

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
	
	// === 核心修改：记录此文件路径 ===
	tracker.Add(localFilePath)
	// ===========================

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