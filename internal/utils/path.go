package utils

import (
	"path"
	"strings"
)

// JoinPath 拼接路径，处理空值、"/"、"0"、多余斜杠等边界情况
// 用于 OpenList/WebDAV 路径拼接
func JoinPath(parts ...string) string {
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
