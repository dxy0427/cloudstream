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
	if threads < 1 {
		threads = 1
	}
	if threads > 8 {
		threads = 8
	}

	log.Info().Str("task", task.Name).Str("account", account.Name).Int("threads", threads).Msg("开始执行任务")
	client := pan123.NewClient(account)
	strmExtMap := parseExtensions(task.StrmExtensions)
	metaExtMap := parseExtensions(task.MetaExtensions)
	var wg sync.WaitGroup

	workerPool := make(chan struct{}, threads)
	rateLimiter := time.NewTicker(time.Second / time.Duration(threads))
	defer rateLimiter.Stop()

	startFolderID := task.SourceFolderID
	if account.Type == models.AccountTypeOpenList && (startFolderID == "0" || startFolderID == "") {
		startFolderID = "/"
	}

	scanDirectoryRecursive(ctx, client, task, account.Type, startFolderID, "", task.LocalPath, strmExtMap, metaExtMap, &wg, workerPool, rateLimiter)

	wg.Wait()
	select {
	case <-ctx.Done():
		log.Warn().Str("task", task.Name).Msg("任务已被手动停止")
	default:
		log.Info().Str("task", task.Name).Msg("任务执行完毕")
	}
}

func scanDirectoryRecursive(ctx context.Context, client *pan123.Client, task models.Task, accountType, folderID, currentCloudPath, localBasePath string, strmExtMap, metaExtMap map[string]bool, wg *sync.WaitGroup, pool chan struct{}, limiter *time.Ticker) {
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
				scanDirectoryRecursive(ctx, client, task, accountType, nextFolderID, itemCloudPath, nextLocalPath, strmExtMap, metaExtMap, wg, pool, limiter)
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
					createStrmFile(client, task, fileToProcess, cloudRelPath, localBasePath)
				} else if metaExtMap[ext] {
					var downloadIdentity interface{}
					if accountType == models.AccountTypeOpenList {
						downloadIdentity = joinOpenListPath(folderID, fileToProcess.FileName)
					} else {
						downloadIdentity = fileToProcess.FileId
					}
					downloadAndSaveMetaFile(client, task, downloadIdentity, fileToProcess.FileName, localBasePath)
				}
			}(currentItem, itemCloudPath)
		}
	}
}

func createStrmFile(client *pan123.Client, task models.Task, file pan123.FileInfo, cloudRelPath string, localBasePath string) {
	fileNameWithoutExt := strings.TrimSuffix(file.FileName, filepath.Ext(file.FileName))
	strmFileName := fileNameWithoutExt + ".strm"
	localFilePath := filepath.Join(localBasePath, strmFileName)

	// 如果文件已存在且不覆盖，直接跳过（不打印日志）
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

func downloadAndSaveMetaFile(client *pan123.Client, task models.Task, identity interface{}, fileName string, localBasePath string) {
	localFilePath := filepath.Join(localBasePath, fileName)
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