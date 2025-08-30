package logger

import (
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/natefinch/lumberjack.v2"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Init：初始化全局日志（集成lumberjack日志轮换，支持控制台+文件双输出）
func Init() {
	// 日志文件路径：与数据库同目录（./data），便于持久化管理
	logFilePath := filepath.Join("./data", "cloudstream.log")

	// 配置lumberjack日志轮换（避免单文件过大，自动清理旧日志）
	lumberjackLogger := &lumberjack.Logger{
		Filename:   logFilePath, // 日志文件存储路径
		MaxSize:    10,          // 单文件最大大小（单位：MB）
		MaxBackups: 3,           // 保留旧日志文件最大数量
		MaxAge:     28,          // 旧日志文件最大保留天数
		Compress:   true,        // 压缩旧日志（节省存储空间）
	}
	
	// 配置双输出：
	// 1. 控制台：美化格式（带时间戳），供docker logs等实时查看
	// 2. 文件：JSON原始格式，通过lumberjack自动轮换持久化
	consoleWriter := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}
	multiWriter := io.MultiWriter(consoleWriter, lumberjackLogger)

	// 初始化全局logger：双输出+自动加时间戳，日志级别设为Info
	log.Logger = zerolog.New(multiWriter).With().Timestamp().Logger()
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	log.Info().Str("path", logFilePath).Msg("日志系统已初始化，集成lumberjack日志轮换")
}