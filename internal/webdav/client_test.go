package webdav

import (
	"cloudstream/internal/models"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestListDirectoryContextCancelsRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})

	client := NewClient(models.Account{WebDAVURL: server.URL})
	client.MetadataHTTPClient = server.Client()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := client.ListDirectoryContext(ctx, "/")
		result <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("PROPFIND request did not reach the server")
	}
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context cancellation, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WebDAV request did not return promptly after cancellation")
	}
}

func TestDirectoryCacheUsesConfigurationFingerprint(t *testing.T) {
	const accountID = 7301
	InvalidateAccountCache(accountID)
	t.Cleanup(func() { InvalidateAccountCache(accountID) })

	oldStarted := make(chan struct{})
	releaseOld := make(chan struct{})
	oldServer, oldRequests := newDAVServer(t, "old.mkv", oldStarted, releaseOld)
	t.Cleanup(oldServer.Close)
	newServer, newRequests := newDAVServer(t, "new.mkv", nil, nil)
	t.Cleanup(newServer.Close)

	oldAccount := models.Account{
		WebDAVURL:      oldServer.URL,
		WebDAVUsername: "old-user",
		WebDAVPassword: "old-secret",
		CacheTTL:       1,
	}
	oldAccount.ID = accountID
	newAccount := models.Account{
		WebDAVURL:      newServer.URL,
		WebDAVUsername: "new-user",
		WebDAVPassword: "new-secret",
		CacheTTL:       1,
	}
	newAccount.ID = accountID
	oldClient := NewClient(oldAccount)
	oldClient.MetadataHTTPClient = oldServer.Client()
	newClient := NewClient(newAccount)
	newClient.MetadataHTTPClient = newServer.Client()

	type listResult struct {
		files []FileInfo
		err   error
	}
	oldResult := make(chan listResult, 1)
	go func() {
		files, err := oldClient.ListDirectoryContext(context.Background(), "/")
		oldResult <- listResult{files: files, err: err}
	}()
	<-oldStarted
	InvalidateAccountCache(accountID)

	newFiles, err := newClient.ListDirectoryContext(context.Background(), "/")
	if err != nil {
		t.Fatalf("list new generation: %v", err)
	}
	if len(newFiles) != 1 || newFiles[0].Name != "new.mkv" {
		t.Fatalf("unexpected new-generation files: %#v", newFiles)
	}
	close(releaseOld)
	old := <-oldResult
	if old.err != nil {
		t.Fatalf("list old generation: %v", old.err)
	}
	if len(old.files) != 1 || old.files[0].Name != "old.mkv" {
		t.Fatalf("unexpected old-generation files: %#v", old.files)
	}

	newFiles, err = newClient.ListDirectoryContext(context.Background(), "/")
	if err != nil {
		t.Fatalf("list cached new generation: %v", err)
	}
	if len(newFiles) != 1 || newFiles[0].Name != "new.mkv" {
		t.Fatalf("old generation polluted new directory cache: %#v", newFiles)
	}
	if got := newRequests.Load(); got != 2 {
		t.Fatalf("expected one AutoAuth list operation (2 requests), got %d requests", got)
	}
	if got := oldRequests.Load(); got != 2 {
		t.Fatalf("expected one old-generation AutoAuth list operation (2 requests), got %d requests", got)
	}
}

func newDAVServer(t *testing.T, name string, started chan struct{}, release <-chan struct{}) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if started != nil {
			once.Do(func() { close(started) })
		}
		if release != nil {
			<-release
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = fmt.Fprint(w, davListResponse(name))
	}))
	return server, &requests
}

func davListResponse(name string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<d:multistatus xmlns:d="DAV:">
  <d:response>
    <d:href>/</d:href>
    <d:propstat>
      <d:prop>
        <d:displayname>/</d:displayname>
        <d:resourcetype><d:collection/></d:resourcetype>
        <d:getcontentlength>0</d:getcontentlength>
        <d:getlastmodified>Tue, 14 Jul 2026 00:00:00 GMT</d:getlastmodified>
      </d:prop>
      <d:status>HTTP/1.1 200 OK</d:status>
    </d:propstat>
  </d:response>
  <d:response>
    <d:href>/` + name + `</d:href>
    <d:propstat>
      <d:prop>
        <d:displayname>` + name + `</d:displayname>
        <d:resourcetype/>
        <d:getcontentlength>1</d:getcontentlength>
        <d:getlastmodified>Tue, 14 Jul 2026 00:00:00 GMT</d:getlastmodified>
      </d:prop>
      <d:status>HTTP/1.1 200 OK</d:status>
    </d:propstat>
  </d:response>
</d:multistatus>`
}
