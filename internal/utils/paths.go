package utils

import (
	"path/filepath"
	"strings"
)

func PathsOverlap(first, second string) bool {
	first = canonicalPath(first)
	second = canonicalPath(second)
	if first == second {
		return true
	}
	firstToSecond, err := filepath.Rel(first, second)
	if err == nil && firstToSecond != ".." && !strings.HasPrefix(firstToSecond, ".."+string(filepath.Separator)) {
		return true
	}
	secondToFirst, err := filepath.Rel(second, first)
	return err == nil && secondToFirst != ".." && !strings.HasPrefix(secondToFirst, ".."+string(filepath.Separator))
}

func canonicalPath(path string) string {
	path = filepath.Clean(path)
	current := path
	remaining := ""
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			if remaining != "" {
				resolved = filepath.Join(resolved, remaining)
			}
			return filepath.Clean(resolved)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return path
		}
		base := filepath.Base(current)
		if remaining == "" {
			remaining = base
		} else {
			remaining = filepath.Join(base, remaining)
		}
		current = parent
	}
}
