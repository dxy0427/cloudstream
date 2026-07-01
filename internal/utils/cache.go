package utils

import (
	"path"
	"strconv"
	"strings"
)

// GetCacheTTL 根据自定义缓存策略返回指定路径的 TTL（分钟）
// 格式：每行一条 "glob模式:分钟"，如 /tv/*:10
// 匹配到第一条即返回，未匹配则用默认 CacheTTL
func GetCacheTTL(policies string, defaultTTL int, dirPath string) int {
	if policies == "" {
		return defaultTTL
	}
	for _, line := range strings.Split(policies, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pattern, ttlStr, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if matched, _ := path.Match(pattern, dirPath); matched {
			if ttl, err := strconv.Atoi(strings.TrimSpace(ttlStr)); err == nil {
				return ttl
			}
		}
	}
	return defaultTTL
}
