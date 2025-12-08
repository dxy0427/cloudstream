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

func Init() {
	logFilePath := filepath.Join("./data", "cloudstream.log")

	lumberjackLogger := &lumberjack.Logger{
		Filename:   logFilePath,
		MaxSize:    10,
		MaxBackups: 3,
		MaxAge:     28,
		Compress:   true,
	}

	consoleWriter := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}
	multiWriter := io.MultiWriter(consoleWriter, lumberjackLogger)

	log.Logger = zerolog.New(multiWriter).With().Timestamp().Logger()
	
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	log.Info().Str("path", logFilePath).Msg("日志系统已初始化")
}