package core

import (
	"bytes"
	"context"
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

const (
	maxLogLineBytes   = 256 * 1024
	maxRecentLogLines = 200
	logReadChunkBytes = 64 * 1024
)

type LogCursor struct {
	Offset   int64
	FileInfo os.FileInfo
}

func tailLines(path string, maxLines int) ([]string, LogCursor, error) {
	return tailLinesContext(context.Background(), path, maxLines)
}

func tailLinesContext(ctx context.Context, path string, maxLines int) ([]string, LogCursor, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, LogCursor{}, nil
		}
		return nil, LogCursor{}, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, LogCursor{}, err
	}
	lines, err := readTailLogLines(ctx, file, 0, info.Size(), maxLines)
	if err != nil {
		return nil, LogCursor{}, err
	}
	return lines, LogCursor{Offset: info.Size(), FileInfo: info}, nil
}

func ReadRecentLogs() ([]string, error) {
	lines, _, err := tailLines(LogFilePath, maxRecentLogLines)
	return lines, err
}

func ReadRecentLogsWithCursor() ([]string, LogCursor, error) {
	return ReadRecentLogsWithCursorContext(context.Background())
}

func ReadRecentLogsWithCursorContext(ctx context.Context) ([]string, LogCursor, error) {
	return tailLinesContext(ctx, LogFilePath, maxRecentLogLines)
}

func ReadLogsFromCursor(cursor LogCursor) ([]string, LogCursor, error) {
	return ReadLogsFromCursorContext(context.Background(), cursor)
}

func ReadLogsFromCursorContext(ctx context.Context, cursor LogCursor) ([]string, LogCursor, error) {
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
	startOffset := cursor.Offset
	if cursor.FileInfo == nil || !os.SameFile(cursor.FileInfo, info) || startOffset < 0 || startOffset > info.Size() {
		startOffset = 0
	}
	lines, err := readTailLogLines(ctx, file, startOffset, info.Size(), maxRecentLogLines)
	if err != nil {
		return nil, cursor, err
	}
	return lines, LogCursor{Offset: info.Size(), FileInfo: info}, nil
}

func readTailLogLines(ctx context.Context, file *os.File, startOffset, endOffset int64, maxLines int) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maxLines <= 0 || endOffset <= startOffset {
		return []string{}, nil
	}

	position := endOffset
	newlineCount := 0
	chunks := make([][]byte, 0, 4)
	for position > startOffset && newlineCount <= maxLines {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		readSize := int64(logReadChunkBytes)
		if available := position - startOffset; available < readSize {
			readSize = available
		}
		readOffset := position - readSize
		chunk := make([]byte, int(readSize))
		n, err := file.ReadAt(chunk, readOffset)
		if err != nil && err != io.EOF {
			return nil, err
		}
		if n == 0 {
			break
		}
		chunk = chunk[:n]
		chunks = append(chunks, chunk)
		newlineCount += bytes.Count(chunk, []byte{'\n'})
		position = readOffset
	}

	totalBytes := 0
	for _, chunk := range chunks {
		totalBytes += len(chunk)
	}
	data := make([]byte, 0, totalBytes)
	for i := len(chunks) - 1; i >= 0; i-- {
		data = append(data, chunks[i]...)
	}
	parts := bytes.Split(data, []byte{'\n'})
	lines := make([]string, 0, maxLines)
	for i := len(parts) - 1; i >= 0 && len(lines) < maxLines; i-- {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if i == len(parts)-1 && len(parts[i]) == 0 {
			continue
		}
		if i == 0 && position > startOffset {
			break
		}
		line := bytes.TrimSuffix(parts[i], []byte{'\r'})
		if len(line) > maxLogLineBytes {
			line = line[:maxLogLineBytes]
		}
		formatted := normalizeLogLine(string(line))
		if formatted != "" {
			lines = append(lines, formatted)
		}
	}
	for left, right := 0, len(lines)-1; left < right; left, right = left+1, right-1 {
		lines[left], lines[right] = lines[right], lines[left]
	}
	return lines, nil
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
