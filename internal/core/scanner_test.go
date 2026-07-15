package core

import (
	"cloudstream/internal/auth"
	"cloudstream/internal/models"
	"cloudstream/internal/pan123"
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
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

func TestCreateStrmFileUsesAccountSigningConfiguration(t *testing.T) {
	rootPath := t.TempDir()
	outputRoot, err := openScanOutputRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer outputRoot.root.Close()

	client := pan123.NewClient(models.Account{StrmBaseURL: "https://stream.example"})
	file := pan123.FileInfo{FileId: 42, FileName: "movie.mkv"}

	publicTask := models.Task{AccountID: 7, SourceFolderID: "0"}
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

	signedClient := pan123.NewClient(models.Account{
		StrmBaseURL:      "https://stream.example",
		EnableStreamSign: true,
		SignExpireHours:  2,
	})
	signedTask := models.Task{AccountID: 7, SourceFolderID: "0"}
	signedTask.ID = 9
	before := time.Now().Add(2 * time.Hour).Unix()
	if err := createStrmFile(context.Background(), signedClient, models.AccountType123Pan, outputRoot, signedTask, file, "movie.mkv", "signed", NewFileTracker(), &ScanStats{}); err != nil {
		t.Fatal(err)
	}
	after := time.Now().Add(2 * time.Hour).Unix()
	signedContent, err := outputRoot.root.ReadFile(filepath.Join("signed", "movie.strm"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(signedContent), "https://stream.example/api/v1/stream/s/movie.mkv?sign=") {
		t.Fatalf("signed STRM = %q", signedContent)
	}
	signedURL, err := url.Parse(string(signedContent))
	if err != nil {
		t.Fatal(err)
	}
	sign := signedURL.Query().Get("sign")
	accountID, identity, err := auth.VerifyAccountStreamSign(sign)
	if err != nil {
		t.Fatal(err)
	}
	if accountID != 7 || identity != "42" {
		t.Fatalf("accountID=%d identity=%q", accountID, identity)
	}
	signParts := strings.Split(sign, ":")
	expiresAt, err := strconv.ParseInt(signParts[1], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if expiresAt < before || expiresAt > after {
		t.Fatalf("signature expiry = %d, want between %d and %d", expiresAt, before, after)
	}
}
