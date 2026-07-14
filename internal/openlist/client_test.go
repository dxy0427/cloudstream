package openlist

import (
	"cloudstream/internal/models"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestContextRequestsCancelPromptly(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		account models.Account
		request func(context.Context, *Client) error
	}{
		{
			name: "login",
			path: "/api/auth/login",
			account: models.Account{
				OpenListAuthMode: "password",
				OpenListUsername: "user",
				OpenListPassword: "secret",
			},
			request: func(ctx context.Context, client *Client) error {
				_, err := client.ListDirectoryContext(ctx, "/", false)
				return err
			},
		},
		{
			name: "list",
			path: "/api/fs/list",
			account: models.Account{
				OpenListAuthMode: "token",
				OpenListToken:    "token",
			},
			request: func(ctx context.Context, client *Client) error {
				_, err := client.ListDirectoryContext(ctx, "/", false)
				return err
			},
		},
		{
			name: "get",
			path: "/api/fs/get",
			account: models.Account{
				OpenListAuthMode: "token",
				OpenListToken:    "token",
			},
			request: func(ctx context.Context, client *Client) error {
				_, err := client.GetRawURLContext(ctx, "/file.mkv")
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tt.path {
					t.Errorf("unexpected request path: %s", r.URL.Path)
				}
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

			tt.account.OpenListURL = server.URL
			client := NewClient(tt.account)
			client.HTTPClient = server.Client()
			ctx, cancel := context.WithCancel(context.Background())
			result := make(chan error, 1)
			go func() {
				result <- tt.request(ctx, client)
			}()

			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("request did not reach the server")
			}
			cancel()

			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("expected context cancellation, got %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("request did not return promptly after cancellation")
			}
		})
	}
}

func TestNewClientInfersLegacyEmptyAuthMode(t *testing.T) {
	tokenClient := NewClient(models.Account{OpenListToken: "token"})
	if tokenClient.AuthMode != "token" || tokenClient.StaticToken != "token" {
		t.Fatalf("legacy token account was not normalized: %+v", tokenClient)
	}
	passwordClient := NewClient(models.Account{OpenListUsername: "user", OpenListPassword: "secret"})
	if passwordClient.AuthMode != "password" || passwordClient.Username != "user" || passwordClient.Password != "secret" {
		t.Fatalf("legacy password account was not normalized: %+v", passwordClient)
	}
}

func TestRetrySleepIsContextAware(t *testing.T) {
	var attempts atomic.Int32
	attempted := make(chan struct{})
	var once sync.Once
	client := &Client{
		AuthMode:    "token",
		StaticToken: "token",
		BaseURL:     "http://openlist.invalid",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts.Add(1)
			once.Do(func() { close(attempted) })
			return nil, errors.New("temporary network error")
		})},
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := client.GetRawURLContext(ctx, "/file.mkv")
		result <- err
	}()
	<-attempted
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context cancellation, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("retry backoff did not stop after cancellation")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("expected one request attempt, got %d", got)
	}
}

func TestTokenCacheUsesConfigurationFingerprint(t *testing.T) {
	const accountID = 7101
	InvalidateAccountCache(accountID)
	t.Cleanup(func() { InvalidateAccountCache(accountID) })

	oldStarted := make(chan struct{})
	releaseOld := make(chan struct{})
	oldServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(oldStarted)
		<-releaseOld
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]string{"token": "old-token"},
		})
	}))
	t.Cleanup(oldServer.Close)

	var newRequests atomic.Int32
	newServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		newRequests.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]string{"token": "new-token"},
		})
	}))
	t.Cleanup(newServer.Close)

	oldAccount := models.Account{
		OpenListURL:      oldServer.URL,
		OpenListAuthMode: "password",
		OpenListUsername: "old-user",
		OpenListPassword: "old-secret",
	}
	oldAccount.ID = accountID
	newAccount := models.Account{
		OpenListURL:      newServer.URL,
		OpenListAuthMode: "password",
		OpenListUsername: "new-user",
		OpenListPassword: "new-secret",
	}
	newAccount.ID = accountID
	oldClient := NewClient(oldAccount)
	oldClient.HTTPClient = oldServer.Client()
	newClient := NewClient(newAccount)
	newClient.HTTPClient = newServer.Client()

	type tokenResult struct {
		token string
		err   error
	}
	oldResult := make(chan tokenResult, 1)
	go func() {
		token, err := oldClient.getTokenContext(context.Background(), oldClient.cacheFingerprint())
		oldResult <- tokenResult{token: token, err: err}
	}()
	<-oldStarted
	InvalidateAccountCache(accountID)
	close(releaseOld)
	old := <-oldResult
	if old.err != nil {
		t.Fatalf("get old token: %v", old.err)
	}
	if old.token != "old-token" {
		t.Fatalf("expected old token, got %q", old.token)
	}

	newToken, err := newClient.getTokenContext(context.Background(), newClient.cacheFingerprint())
	if err != nil {
		t.Fatalf("get new token: %v", err)
	}
	if newToken != "new-token" {
		t.Fatalf("expected new token, got %q", newToken)
	}

	newToken, err = newClient.getTokenContext(context.Background(), newClient.cacheFingerprint())
	if err != nil {
		t.Fatalf("get cached new token: %v", err)
	}
	if newToken != "new-token" {
		t.Fatalf("old generation polluted new token cache: %q", newToken)
	}
	if got := newRequests.Load(); got != 1 {
		t.Fatalf("expected one new-generation login, got %d", got)
	}

	oldKey := oldClient.tokenCacheKey(oldClient.cacheFingerprint())
	newKey := newClient.tokenCacheKey(newClient.cacheFingerprint())
	cacheMutex.RLock()
	_, oldCached := globalTokenCache[oldKey]
	_, newCached := globalTokenCache[newKey]
	cacheMutex.RUnlock()
	if oldCached || !newCached {
		t.Fatalf("expected only the active generation to remain cached, old=%v new=%v", oldCached, newCached)
	}
}

func TestDirectoryCacheUsesConfigurationFingerprint(t *testing.T) {
	const accountID = 7102
	InvalidateAccountCache(accountID)
	t.Cleanup(func() { InvalidateAccountCache(accountID) })

	oldStarted := make(chan struct{})
	releaseOld := make(chan struct{})
	oldServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(oldStarted)
		<-releaseOld
		writeListResponse(t, w, "old.mkv")
	}))
	t.Cleanup(oldServer.Close)

	var newRequests atomic.Int32
	newServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		newRequests.Add(1)
		writeListResponse(t, w, "new.mkv")
	}))
	t.Cleanup(newServer.Close)

	oldAccount := models.Account{
		OpenListURL:      oldServer.URL,
		OpenListAuthMode: "token",
		OpenListToken:    "old-token",
		CacheTTL:         1,
	}
	oldAccount.ID = accountID
	newAccount := models.Account{
		OpenListURL:      newServer.URL,
		OpenListAuthMode: "token",
		OpenListToken:    "new-token",
		CacheTTL:         1,
	}
	newAccount.ID = accountID
	oldClient := NewClient(oldAccount)
	oldClient.HTTPClient = oldServer.Client()
	newClient := NewClient(newAccount)
	newClient.HTTPClient = newServer.Client()

	type listResult struct {
		files []FileInfo
		err   error
	}
	oldResult := make(chan listResult, 1)
	go func() {
		files, err := oldClient.ListDirectoryContext(context.Background(), "/", false)
		oldResult <- listResult{files: files, err: err}
	}()
	<-oldStarted
	InvalidateAccountCache(accountID)
	close(releaseOld)
	old := <-oldResult
	if old.err != nil {
		t.Fatalf("list old generation: %v", old.err)
	}
	if len(old.files) != 1 || old.files[0].Name != "old.mkv" {
		t.Fatalf("unexpected old-generation files: %#v", old.files)
	}

	newFiles, err := newClient.ListDirectoryContext(context.Background(), "/", false)
	if err != nil {
		t.Fatalf("list new generation: %v", err)
	}
	if len(newFiles) != 1 || newFiles[0].Name != "new.mkv" {
		t.Fatalf("unexpected new-generation files: %#v", newFiles)
	}

	newFiles, err = newClient.ListDirectoryContext(context.Background(), "/", false)
	if err != nil {
		t.Fatalf("list cached new generation: %v", err)
	}
	if len(newFiles) != 1 || newFiles[0].Name != "new.mkv" {
		t.Fatalf("old generation polluted new directory cache: %#v", newFiles)
	}
	if got := newRequests.Load(); got != 1 {
		t.Fatalf("expected one new-generation list request, got %d", got)
	}
}

func writeListResponse(t *testing.T, w http.ResponseWriter, name string) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(map[string]any{
		"code": 200,
		"data": map[string]any{
			"content": []map[string]any{{"name": name, "size": 1, "is_dir": false}},
			"total":   1,
		},
	}); err != nil {
		t.Errorf("write response: %v", err)
	}
}
