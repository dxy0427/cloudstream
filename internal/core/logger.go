package core

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
)

// LogFilePath 与 internal/logger/logger.go 中保持一致
const LogFilePath = "./data/cloudstream.log"

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

	if strings.HasPrefix(line, "{") && strings.HasSuffix(line, "}") {
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(line), &obj); err == nil {
			return formatJSONLog(obj)
		}
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

func formatJSONLog(obj map[string]interface{}) string {
	level := "INFO"
	if v, ok := obj["level"].(string); ok && v != "" {
		level = strings.ToUpper(v)
	}

	timestamp := "-"
	if v, ok := obj["time"].(string); ok && v != "" {
		timestamp = v
	}

	message := ""
	if v, ok := obj["message"].(string); ok {
		message = v
	}

	extra := make([]string, 0)
	keys := make([]string, 0, len(obj))
	for k := range obj {
		if k == "level" || k == "time" || k == "message" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		extra = append(extra, fmt.Sprintf("%s=%v", k, obj[k]))
	}

	if len(extra) > 0 && message != "" {
		message = fmt.Sprintf("%s | %s", strings.Join(extra, " "), message)
	} else if len(extra) > 0 {
		message = strings.Join(extra, " ")
	}

	return fmt.Sprintf("[%s] [%s] %s", timestamp, level, strings.TrimSpace(message))
}
