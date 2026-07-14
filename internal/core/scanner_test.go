package core

import (
	"cloudstream/internal/models"
	"cloudstream/internal/pan123"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanOutputRootRejectsEscapes(t *testing.T) {
	rootPath := t.TempDir()
	outsidePath := t.TempDir()
	outputRoot, err := openScanOutputRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer outputRoot.root.Close()

	if _, err := outputRoot.absolutePath(filepath.Join("..", "outside.strm")); err == nil {
		t.Fatal("parent traversal was accepted")
	}
	if _, ok := outputRoot.relativeFromAbsolute(filepath.Join(outsidePath, "outside.strm")); ok {
		t.Fatal("outside absolute path was accepted")
	}

	if err := os.Symlink(outsidePath, filepath.Join(rootPath, "escape")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	err = outputRoot.replaceFile(context.Background(), filepath.Join("escape", "outside.strm"), 0644, func(writer io.Writer) error {
		_, err := io.WriteString(writer, "unsafe")
		return err
	})
	if err == nil {
		t.Fatal("symlink escape was accepted")
	}
	if _, err := os.Stat(filepath.Join(outsidePath, "outside.strm")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside file was created: %v", err)
	}
}

func TestScanOutputRootCanceledReplacePreservesOldFile(t *testing.T) {
	rootPath := t.TempDir()
	outputRoot, err := openScanOutputRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer outputRoot.root.Close()

	const fileName = "video.strm"
	if err := outputRoot.root.WriteFile(fileName, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	err = outputRoot.replaceFile(ctx, fileName, 0644, func(writer io.Writer) error {
		if _, err := io.WriteString(writer, "new"); err != nil {
			return err
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("replace error = %v, want context canceled", err)
	}
	content, err := outputRoot.root.ReadFile(fileName)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old" {
		t.Fatalf("file content = %q, want old", content)
	}
}

func TestScanOutputRootStoresResolvedRootPath(t *testing.T) {
	realRoot := t.TempDir()
	parent := t.TempDir()
	link := filepath.Join(parent, "output")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	outputRoot, err := openScanOutputRoot(link)
	if err != nil {
		t.Fatal(err)
	}
	defer outputRoot.root.Close()
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatal(err)
	}
	if outputRoot.absPath != resolved {
		t.Fatalf("root path = %q, want resolved path %q", outputRoot.absPath, resolved)
	}
}

func TestCreateStrmFileKeepsSignedAndPublicModes(t *testing.T) {
	rootPath := t.TempDir()
	outputRoot, err := openScanOutputRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer outputRoot.root.Close()

	clientAccount := models.Account{StrmBaseURL: "https://stream.example"}
	client := pan123.NewClient(clientAccount)
	file := pan123.FileInfo{FileId: 42, FileName: "movie.mkv"}

	publicTask := models.Task{AccountID: 7, SourceFolderID: "0", EncodePath: false}
	if err := createStrmFile(context.Background(), client, models.AccountType123Pan, outputRoot, publicTask, file, "movie.mkv", "public", NewFileTracker(), &ScanStats{}); err != nil {
		t.Fatal(err)
	}
	publicContent, err := outputRoot.root.ReadFile(filepath.Join("public", "movie.strm"))
	if err != nil {
		t.Fatal(err)
	}
	if string(publicContent) != "https://stream.example/api/v1/stream/s/7/42/movie.mkv" {
		t.Fatalf("public STRM = %q", publicContent)
	}

	signedTask := models.Task{AccountID: 7, SourceFolderID: "0", EncodePath: true}
	signedTask.ID = 9
	if err := createStrmFile(context.Background(), client, models.AccountType123Pan, outputRoot, signedTask, file, "movie.mkv", "signed", NewFileTracker(), &ScanStats{}); err != nil {
		t.Fatal(err)
	}
	signedContent, err := outputRoot.root.ReadFile(filepath.Join("signed", "movie.strm"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(signedContent), "https://stream.example/api/v1/stream/s/movie.mkv?sign=") {
		t.Fatalf("signed STRM = %q", signedContent)
	}
}
