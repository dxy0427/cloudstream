package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTailLinesReturnsOnlyMostRecentLogs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cloudstream.log")
	var content strings.Builder
	content.WriteString(strings.Repeat("old-prefix", 10_000))
	content.WriteByte('\n')
	for i := 0; i < 250; i++ {
		fmt.Fprintf(&content, "{\"level\":\"info\",\"time\":\"2026-07-17T04:00:00+08:00\",\"message\":\"line-%03d\"}\n", i)
	}
	if err := os.WriteFile(path, []byte(content.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	lines, cursor, err := tailLines(path, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 200 {
		t.Fatalf("line count = %d, want 200", len(lines))
	}
	if got, want := lines[0], "[2026-07-17T04:00:00+08:00] [INFO] line-050"; got != want {
		t.Fatalf("first line = %q, want %q", got, want)
	}
	if got, want := lines[len(lines)-1], "[2026-07-17T04:00:00+08:00] [INFO] line-249"; got != want {
		t.Fatalf("last line = %q, want %q", got, want)
	}
	if cursor.Offset != int64(content.Len()) || cursor.FileInfo == nil {
		t.Fatalf("cursor = %+v, want EOF %d with file info", cursor, content.Len())
	}
}

func TestReadTailLogLinesReadsAppendedRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cloudstream.log")
	initial := "old-1\nold-2\n"
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	if _, err := file.WriteString("new-1\r\nnew-2"); err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	lines, err := readTailLogLines(context.Background(), file, int64(len(initial)), info.Size(), 200)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"[-] [INFO] new-1", "[-] [INFO] new-2"}
	if fmt.Sprint(lines) != fmt.Sprint(want) {
		t.Fatalf("lines = %#v, want %#v", lines, want)
	}
}

func TestReadTailLogLinesHonorsCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cloudstream.log")
	if err := os.WriteFile(path, []byte("line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = readTailLogLines(ctx, file, 0, 5, 200)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
