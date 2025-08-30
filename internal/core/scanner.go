package core

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/pan123"
	"context"
	"github.com/rs/zerolog/log"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RunScanTask：执行扫描任务（拉取云盘文件，生成STRM播放文件/元数据文件，支持并发控制）
func RunScanTask(ctx context.Context, task models.Task) {
	// 任务结束后释放运行状态（从runningTasks中移除）
	defer func() {
		taskMutex.Lock()
		delete(runningTasks, task.ID)
		taskMutex.Unlock()
		log.Info().Str("task", task.Name).Msg("任务控制权已释放")
	}()

	// 查询任务关联的云账户，不存在则启动失败
	var account models.Account
	if err := database.DB.First(&account, task.AccountID).Error; err != nil {
		log.Error().Err(err).Str("task", task.Name).Uint("accountID", task.AccountID).Msg("任务启动失败：找不到关联的云账户")
		return
	}

	log.Info().Str("task", task.Name).Str("account", account.Name).Int("threads", task.Threads).Msg("开始执行任务")
	client := pan123.NewClient(account)
	// 解析STRM/元数据文件后缀（转成map便于快速匹配）
	strmExtMap := parseExtensions(task.StrmExtensions)
	metaExtMap := parseExtensions(task.MetaExtensions)

	var wg sync.WaitGroup
	// 线程数限制在1-8之间（避免并发过高导致云盘接口限流）
	threads := task.Threads
	if threads < 1 {
		threads = 1
	}
	if threads > 8 {
		threads = 8
	}
	// 协程池控制并发数，速率限制控制每秒请求数
	workerPool := make(chan struct{}, threads)
	rateLimiter := time.NewTicker(time.Second / time.Duration(threads))
	defer rateLimiter.Stop()

	// 递归扫描云盘目录，处理文件
	scanDirectory(ctx, client, task, task.SourceFolderID, task.LocalPath, strmExtMap, metaExtMap, &wg, workerPool, rateLimiter)
	wg.Wait()

	// 检查任务是否被手动停止
	select {
	case <-ctx.Done():
		log.Warn().Str("task", task.Name).Msg("任务已被手动停止")
	default:
		log.Info().Str("task", task.Name).Msg("任务执行完毕")
	}
}

// scanDirectory：递归扫描云盘目录（区分文件夹/文件，并发处理文件生成/下载）
func scanDirectory(ctx context.Context, client *pan123.Client, task models.Task, folderID, localBasePath string, strmExtMap, metaExtMap map[string]bool, wg *sync.WaitGroup, pool chan struct{}, limiter *time.Ticker) {
	// 任务被停止则直接返回
	select {
	case <-ctx.Done():
		return
	default:
	}

	// 转换目录ID为int64（云盘接口要求），无效则报错
	folderIDInt, err := strconv.ParseInt(folderID, 10, 64)
	if err != nil {
		log.Error().Err(err).Str("task", task.Name).Str("folderID", folderID).Msg("无效的目录ID")
		return
	}

	// 分页拉取目录下所有文件（100条/页，直到无更多数据）
	var allFiles []pan123.FileInfo
	var lastFileId int64 = 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		files, nextLastFileId, err := client.ListFiles(folderIDInt, 100, lastFileId)
		if err != nil {
			log.Error().Err(err).Str("task", task.Name).Int64("folderID", folderIDInt).Msg("扫描目录失败")
			return
		}
		allFiles = append(allFiles, files...)
		// nextLastFileId=-1表示无更多文件，退出循环
		if nextLastFileId == -1 {
			break
		}
		lastFileId = nextLastFileId
	}

	// 遍历文件：文件夹递归扫描，文件按类型处理（STRM/元数据）
	for _, item := range allFiles {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if item.IsDir() {
			// 子文件夹：递归扫描，更新目录ID和本地路径
			nextFolderID := strconv.FormatInt(item.FileId, 10)
			nextLocalPath := filepath.Join(localBasePath, item.FileName)
			scanDirectory(ctx, client, task, nextFolderID, nextLocalPath, strmExtMap, metaExtMap, wg, pool, limiter)
		} else {
			// 文件：协程处理（受协程池和速率限制控制）
			wg.Add(1)
			go func(file pan123.FileInfo) {
				defer wg.Done()
				// 任务停止则退出
				select {
				case <-ctx.Done():
					return
				case pool <- struct{}{}: // 协程池限流
				}
				defer func() { <-pool }() // 释放协程池资源

				// 速率限制：控制每秒请求数
				select {
				case <-ctx.Done():
					return
				case <-limiter.C:
				}

				// 匹配文件后缀，决定处理类型（STRM/元数据/跳过）
				ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(file.FileName), "."))
				if strmExtMap[ext] {
					createStrmFile(client, task, file, localBasePath)
				} else if metaExtMap[ext] {
					downloadAndSaveMetaFile(client, task, file, localBasePath)
				}
			}(item)
		}
	}
}

// createStrmFile：生成STRM播放文件（写入云文件的播放链接，供播放器识别）
func createStrmFile(client *pan123.Client, task models.Task, file pan123.FileInfo, localBasePath string) {
	// 生成STRM文件名（原文件名去掉后缀，加.strm）
	fileNameWithoutExt := strings.TrimSuffix(file.FileName, filepath.Ext(file.FileName))
	strmFileName := fileNameWithoutExt + ".strm"
	localFilePath := filepath.Join(localBasePath, strmFileName)

	// 非覆盖模式：文件已存在则跳过
	if !task.Overwrite {
		if _, err := os.Stat(localFilePath); err == nil {
			return
		}
	}

	// 生成云文件的播放链接
	fileIDStr := strconv.FormatInt(file.FileId, 10)
	streamURL, err := client.GetStreamURL(task.AccountID, fileIDStr)
	if err != nil {
		log.Error().Err(err).Str("task", task.Name).Str("file", file.FileName).Msg("获取播放链接失败")
		return
	}

	// 创建本地目录（权限0755：所有者读写执行，其他读执行）
	if err := os.MkdirAll(filepath.Dir(localFilePath), 0755); err != nil {
		log.Error().Err(err).Str("task", task.Name).Str("path", filepath.Dir(localFilePath)).Msg("创建目录失败")
		return
	}

	// 写入STRM文件（权限0644：所有者读写，其他读）
	if err := os.WriteFile(localFilePath, []byte(streamURL), 0644); err != nil {
		log.Error().Err(err).Str("task", task.Name).Str("path", localFilePath).Msg("写入 .strm 文件失败")
		return
	}

	log.Debug().Str("task", task.Name).Str("path", localFilePath).Msg("已创建 .strm 文件")
}

// downloadAndSaveMetaFile：下载云盘元数据文件（如封面、字幕，保存到本地对应目录）
func downloadAndSaveMetaFile(client *pan123.Client, task models.Task, file pan123.FileInfo, localBasePath string) {
	// 本地文件路径（与云盘文件名一致）
	localFilePath := filepath.Join(localBasePath, file.FileName)

	// 非覆盖模式：文件已存在则跳过
	if !task.Overwrite {
		if _, err := os.Stat(localFilePath); err == nil {
			return
		}
	}

	// 获取元数据文件的云盘下载链接
	downloadURL, err := client.GetDownloadURL(file.FileId)
	if err != nil {
		log.Error().Err(err).Str("task", task.Name).Str("file", file.FileName).Msg("获取元数据下载链接失败")
		return
	}

	// 发送HTTP请求下载文件
	resp, err := http.Get(downloadURL)
	if err != nil {
		log.Error().Err(err).Str("task", task.Name).Str("file", file.FileName).Msg("下载元数据失败")
		return
	}
	defer resp.Body.Close()

	// 检查HTTP响应状态，非200则报错
	if resp.StatusCode != http.StatusOK {
		log.Error().Str("task", task.Name).Str("file", file.FileName).Str("status", resp.Status).Msg("下载元数据时服务器返回错误")
		return
	}

	// 创建本地目录（权限0755）
	if err := os.MkdirAll(filepath.Dir(localFilePath), 0755); err != nil {
		log.Error().Err(err).Str("task", task.Name).Str("path", filepath.Dir(localFilePath)).Msg("创建元数据目录失败")
		return
	}

	// 创建本地文件并写入下载内容
	outFile, err := os.Create(localFilePath)
	if err != nil {
		log.Error().Err(err).Str("task", task.Name).Str("path", localFilePath).Msg("创建元数据文件失败")
		return
	}
	defer outFile.Close()

	// 复制下载内容到本地文件
	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		log.Error().Err(err).Str("task", task.Name).Str("path", localFilePath).Msg("保存元数据文件失败")
		return
	}

	log.Debug().Str("task", task.Name).Str("path", localFilePath).Msg("已下载元数据文件")
}

// parseExtensions：解析文件后缀字符串为map（如"mp4,mkv"→map["mp4":true, "mkv":true]，用于快速匹配）
func parseExtensions(extStr string) map[string]bool {
	extMap := make(map[string]bool)
	// 按逗号分割后缀，去空格、转小写后存入map
	parts := strings.Split(extStr, ",")
	for _, part := range parts {
		cleanPart := strings.TrimSpace(strings.ToLower(part))
		if cleanPart != "" {
			extMap[cleanPart] = true
		}
	}
	return extMap
}