package core

import (
	"bufio"
	"bytes"
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

const maxLogLineBytes = 256 * 1024

type LogCursor struct {
	Offset   int64
	FileInfo os.FileInfo
}

func tailLines(path string, maxLines int) ([]string, LogCursor, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, LogCursor{}, nil
		}
		return nil, LogCursor{}, err
	}
	defer file.Close()

	lines := make([]string, 0, maxLines)
	if err := readLogLines(file, func(line string) {
		formatted := normalizeLogLine(line)
		if formatted != "" {
			lines = append(lines, formatted)
		}
		if len(lines) > maxLines {
			lines = lines[1:]
		}
	}); err != nil {
		return nil, LogCursor{}, err
	}
	offset, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, LogCursor{}, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, LogCursor{}, err
	}
	return lines, LogCursor{Offset: offset, FileInfo: info}, nil
}

func ReadRecentLogs() ([]string, error) {
	lines, _, err := tailLines(LogFilePath, 300)
	return lines, err
}

func ReadRecentLogsWithCursor() ([]string, LogCursor, error) {
	return tailLines(LogFilePath, 300)
}

func ReadLogsFromCursor(cursor LogCursor) ([]string, LogCursor, error) {
	file, err := os.Open(LogFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, LogCursor{}, nil
		}
		return nil, cursor, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, cursor, err
	}
	offset := cursor.Offset
	if cursor.FileInfo == nil || !os.SameFile(cursor.FileInfo, info) || offset > info.Size() {
		offset = 0
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, cursor, err
	}

	lines := []string{}
	if err := readLogLines(file, func(line string) {
		formatted := normalizeLogLine(line)
		if formatted != "" {
			lines = append(lines, formatted)
		}
	}); err != nil {
		return nil, cursor, err
	}
	newOffset, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return lines, cursor, err
	}
	return lines, LogCursor{Offset: newOffset, FileInfo: info}, nil
}

func readLogLines(reader io.Reader, handle func(string)) error {
	buffered := bufio.NewReaderSize(reader, 64*1024)
	for {
		line, err := readBoundedLogLine(buffered)
		if len(line) > 0 {
			handle(string(bytes.TrimSuffix(line, []byte{'\r'})))
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func readBoundedLogLine(reader *bufio.Reader) ([]byte, error) {
	line := make([]byte, 0, 64*1024)
	for {
		fragment, err := reader.ReadSlice('\n')
		fragment = bytes.TrimSuffix(fragment, []byte{'\n'})
		remaining := maxLogLineBytes - len(line)
		if remaining > 0 {
			if len(fragment) > remaining {
				fragment = fragment[:remaining]
			}
			line = append(line, fragment...)
		}
		if err == nil {
			return line, nil
		}
		if err == io.EOF {
			return line, io.EOF
		}
		if err != bufio.ErrBufferFull {
			return nil, err
		}
	}
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
