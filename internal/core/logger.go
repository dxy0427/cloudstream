package core

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func tailLines(path string, maxLines int) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	lines := make([]string, 0, maxLines)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > maxLines {
			lines = lines[1:]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func ReadRecentLogs() ([]string, error) {
	candidates := []string{
		"./data/cloudstream.log",
		"./cloudstream.log",
	}

	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Size() > 0 {
			return tailLines(path, 200)
		}
	}

	var matched []string
	searchRoots := []string{"./data", "."}
	for _, root := range searchRoots {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return nil
			}
			name := strings.ToLower(info.Name())
			if strings.HasSuffix(name, ".log") || strings.Contains(name, "cloudstream") {
				matched = append(matched, path)
			}
			return nil
		})
	}

	if len(matched) == 0 {
		return []string{"暂无系统日志"}, nil
	}

	sort.Strings(matched)
	for i := len(matched) - 1; i >= 0; i-- {
		path := matched[i]
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Size() > 0 {
			lines, err := tailLines(path, 200)
			if err == nil && len(lines) > 0 {
				return lines, nil
			}
		}
	}

	return []string{"暂无系统日志"}, nil
}
