package core

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/natefinch/lumberjack.v2"
)

const LogFilePath = "./data/cloudstream.log"

var initLoggerOnce sync.Once

func InitLogger() {
	initLoggerOnce.Do(func() {
		if err := os.MkdirAll(filepath.Dir(LogFilePath), 0755); err != nil {
			fmt.Printf("failed to create log dir: %v\n", err)
			return
		}

		writer := zerolog.MultiLevelWriter(
			zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "2006-01-02 15:04:05"},
			&lumberjack.Logger{
				Filename:   LogFilePath,
				MaxSize:    20,
				MaxBackups: 3,
				MaxAge:     7,
				Compress:   false,
			},
		)

		log.Logger = zerolog.New(writer).With().Timestamp().Logger()
	})
}

func tailLines(path string, maxLines int) ([]string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, 0, nil
		}
		return nil, 0, err
	}
	defer file.Close()

	lines := make([]string, 0, maxLines)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		formatted := normalizeLogLine(scanner.Text())
		if formatted != "" {
			lines = append(lines, formatted)
		}
		if len(lines) > maxLines {
			lines = lines[1:]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}
	offset, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		offset = 0
	}
	return lines, offset, nil
}

func ReadRecentLogs() ([]string, error) {
	lines, _, err := tailLines(LogFilePath, 300)
	return lines, err
}

func ReadRecentLogsWithOffset() ([]string, int64, error) {
	return tailLines(LogFilePath, 300)
}

func ReadLogFromOffset(offset int64) ([]string, int64, error) {
	file, err := os.Open(LogFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, 0, nil
		}
		return nil, offset, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, offset, err
	}
	if offset > info.Size() {
		offset = info.Size()
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}

	lines := []string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		formatted := normalizeLogLine(scanner.Text())
		if formatted != "" {
			lines = append(lines, formatted)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, offset, err
	}
	newOffset, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return lines, offset, nil
	}
	return lines, newOffset, nil
}

var (
	levelRegexp = regexp.MustCompile(`(?i)\b(trace|debug|info|warn|warning|error|fatal|panic)\b`)
	timeRegexp  = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}`)
)

func normalizeLogLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	if strings.HasPrefix(line, "[") {
		return line
	}

	level := "INFO"
	if m := levelRegexp.FindStringSubmatch(line); len(m) > 1 {
		level = strings.ToUpper(m[1])
		if level == "WARNING" {
			level = "WARN"
		}
	}

	timestamp := "-"
	if ts := timeRegexp.FindString(line); ts != "" {
		timestamp = ts
	}

	return fmt.Sprintf("[%s] [%s] %s", timestamp, level, line)
}
