package pan123

import (
	"cloudstream/internal/models"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCanceledContextDoesNotSendRequests(t *testing.T) {
	account := models.Account{
		Type:         models.AccountType123Pan,
		ClientID:     "client-id",
		ClientSecret: "secret",
	}
	account.ID = 7201
	var requests atomic.Int32
	client := NewClient(account)
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, errors.New("request should not be sent")
	})}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if _, _, err := client.ListFilesContext(ctx, 0, 100, 0, "/"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled list request, got %v", err)
	}
	if _, err := client.GetDownloadURLContext(ctx, int64(1)); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled download request, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("canceled calls returned too slowly: %s", elapsed)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("expected no HTTP requests, got %d", got)
	}
}

func TestAccessTokenRequestUsesContext(t *testing.T) {
	account := models.Account{
		Type:         models.AccountType123Pan,
		ClientID:     "client-id",
		ClientSecret: "secret",
	}
	account.ID = 7202
	started := make(chan struct{})
	var once sync.Once
	client := NewClient(account)
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		once.Do(func() { close(started) })
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, _, err := client.ListFilesContext(ctx, 0, 100, 0, "/")
		result <- err
	}()
	<-started
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context cancellation, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("token request did not return promptly after cancellation")
	}
}

func TestCachesUseConfigurationFingerprint(t *testing.T) {
	const accountID = 7203
	InvalidateAccountCache(accountID)
	t.Cleanup(func() { InvalidateAccountCache(accountID) })

	oldStarted := make(chan struct{})
	releaseOld := make(chan struct{})
	var oldTokenOnce sync.Once
	var oldTokenRequests atomic.Int32
	var oldListRequests atomic.Int32
	oldTransport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/v1/access_token":
			oldTokenRequests.Add(1)
			oldTokenOnce.Do(func() { close(oldStarted) })
			<-releaseOld
			return jsonResponse(req, `{"code":0,"message":"","data":{"accessToken":"old-token","expiredAt":"2099-01-01T00:00:00Z"}}`), nil
		case "/api/v2/file/list":
			oldListRequests.Add(1)
			return jsonResponse(req, `{"code":0,"message":"","data":{"fileList":[{"fileId":1,"filename":"old.mkv","type":0}],"lastFileId":0}}`), nil
		default:
			return nil, errors.New("unexpected old-generation endpoint")
		}
	})

	var newTokenRequests atomic.Int32
	var newListRequests atomic.Int32
	newTransport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/v1/access_token":
			newTokenRequests.Add(1)
			return jsonResponse(req, `{"code":0,"message":"","data":{"accessToken":"new-token","expiredAt":"2099-01-01T00:00:00Z"}}`), nil
		case "/api/v2/file/list":
			newListRequests.Add(1)
			return jsonResponse(req, `{"code":0,"message":"","data":{"fileList":[{"fileId":2,"filename":"new.mkv","type":0}],"lastFileId":0}}`), nil
		default:
			return nil, errors.New("unexpected new-generation endpoint")
		}
	})

	oldAccount := models.Account{
		Type:         models.AccountType123Pan,
		ClientID:     "old-client",
		ClientSecret: "old-secret",
		CacheTTL:     1,
	}
	oldAccount.ID = accountID
	newAccount := models.Account{
		Type:         models.AccountType123Pan,
		ClientID:     "new-client",
		ClientSecret: "new-secret",
		CacheTTL:     1,
	}
	newAccount.ID = accountID
	oldClient := NewClient(oldAccount)
	oldClient.HTTPClient = &http.Client{Transport: oldTransport}
	newClient := NewClient(newAccount)
	newClient.HTTPClient = &http.Client{Transport: newTransport}

	type listResult struct {
		files []FileInfo
		err   error
	}
	oldResult := make(chan listResult, 1)
	go func() {
		files, _, err := oldClient.ListFilesContext(context.Background(), 0, 100, 0, "/")
		oldResult <- listResult{files: files, err: err}
	}()
	<-oldStarted
	InvalidateAccountCache(accountID)
	close(releaseOld)
	old := <-oldResult
	if old.err != nil {
		t.Fatalf("list old generation: %v", old.err)
	}
	if len(old.files) != 1 || old.files[0].FileName != "old.mkv" {
		t.Fatalf("unexpected old-generation files: %#v", old.files)
	}

	newFiles, _, err := newClient.ListFilesContext(context.Background(), 0, 100, 0, "/")
	if err != nil {
		t.Fatalf("list new generation: %v", err)
	}
	if len(newFiles) != 1 || newFiles[0].FileName != "new.mkv" {
		t.Fatalf("unexpected new-generation files: %#v", newFiles)
	}

	newFiles, _, err = newClient.ListFilesContext(context.Background(), 0, 100, 0, "/")
	if err != nil {
		t.Fatalf("list cached new generation: %v", err)
	}
	if len(newFiles) != 1 || newFiles[0].FileName != "new.mkv" {
		t.Fatalf("old generation polluted new list cache: %#v", newFiles)
	}
	if got := newTokenRequests.Load(); got != 1 {
		t.Fatalf("expected one new-generation token request, got %d", got)
	}
	if got := newListRequests.Load(); got != 1 {
		t.Fatalf("expected one new-generation list request, got %d", got)
	}
	if got := oldTokenRequests.Load(); got != 1 {
		t.Fatalf("expected one old-generation token request, got %d", got)
	}
	if got := oldListRequests.Load(); got != 1 {
		t.Fatalf("expected one old-generation list request, got %d", got)
	}

	oldKey := oldClient.tokenCacheKey(oldClient.cacheFingerprint())
	newKey := newClient.tokenCacheKey(newClient.cacheFingerprint())
	mapMutex.Lock()
	_, oldCached := tokenCaches[oldKey]
	_, newCached := tokenCaches[newKey]
	mapMutex.Unlock()
	if !oldCached || !newCached {
		t.Fatalf("expected both token generations to remain isolated, old=%v new=%v", oldCached, newCached)
	}
}

func jsonResponse(req *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}
