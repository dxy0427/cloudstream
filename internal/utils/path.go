package utils

import (
	"path"
	"strings"
)

// JoinPath 拼接路径，处理空值、根目录和多余斜杠等边界情况
// 用于 OpenList/WebDAV 路径拼接
func JoinPath(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for i, p := range parts {
		if p == "" || (i == 0 && p == "0") {
			continue
		}
		p = strings.Trim(p, "/")
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	result := path.Join(cleaned...)
	if result == "." {
		return "/"
	}
	return "/" + strings.TrimLeft(result, "/")
}
