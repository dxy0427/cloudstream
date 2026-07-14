package handlers

import (
	"cloudstream/internal/auth"
	"cloudstream/internal/models"
	"cloudstream/internal/openlist"
	"cloudstream/internal/webdav"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestExpectedDownstreamDisconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !isExpectedDownstreamDisconnect(ctx, context.Canceled) {
		t.Fatal("canceled request was not recognized")
	}
	if !isExpectedDownstreamDisconnect(context.Background(), syscall.EPIPE) {
		t.Fatal("broken pipe was not recognized")
	}
	if !isExpectedDownstreamDisconnect(context.Background(), syscall.ECONNRESET) {
		t.Fatal("connection reset was not recognized")
	}
	if isExpectedDownstreamDisconnect(context.Background(), errors.New("upstream read failed")) {
		t.Fatal("unexpected upstream error was suppressed")
	}
}

func TestOpenListAccountFromWebDAV(t *testing.T) {
	account, err := openListAccountFromWebDAV(models.Account{
		WebDAVURL:      "https://files.example/base/dav/",
		WebDAVUsername: "user",
		WebDAVPassword: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	schemeLess, err := openListAccountFromWebDAV(models.Account{WebDAVURL: "files.example/base/dav"})
	if err != nil || schemeLess.OpenListURL != "http://files.example/base" {
		t.Fatalf("scheme-less WebDAV conversion failed: %+v, %v", schemeLess, err)
	}
	uppercase, err := openListAccountFromWebDAV(models.Account{WebDAVURL: "HTTPS://files.example/base/dav"})
	if err != nil || !strings.EqualFold(uppercase.OpenListURL, "https://files.example/base") {
		t.Fatalf("uppercase HTTPS conversion failed: %+v, %v", uppercase, err)
	}
	if account.OpenListURL != "https://files.example/base" || account.OpenListAuthMode != "password" || account.OpenListUsername != "user" || account.OpenListPassword != "secret" {
		t.Fatalf("unexpected OpenList account: %+v", account)
	}
	for _, invalid := range []string{
		"https://files.example/webdav",
		"https://files.example/dav?token=secret",
		"ftp://files.example/dav",
		"javascript:alert(1)",
	} {
		if _, err := openListAccountFromWebDAV(models.Account{WebDAVURL: invalid}); err == nil {
			t.Fatalf("accepted invalid OpenList WebDAV URL %q", invalid)
		}
	}
}

func TestValidRedirectURL(t *testing.T) {
	if !validRedirectURL("https://storage.example/video.mkv?sign=value") {
		t.Fatal("valid HTTPS redirect was rejected")
	}
	for _, invalid := range []string{"/relative/video.mkv", "javascript:alert(1)", "file:///tmp/video.mkv"} {
		if validRedirectURL(invalid) {
			t.Fatalf("accepted invalid redirect %q", invalid)
		}
	}
	if resolved, ok := normalizeRedirectURL("/p/video.mkv?sign=value", "https://files.example/base"); !ok || resolved != "https://files.example/p/video.mkv?sign=value" {
		t.Fatalf("relative OpenList URL resolution failed: %q, %v", resolved, ok)
	}
	if resolved, ok := normalizeRedirectURL("p/video.mkv?sign=value", "https://files.example/base"); !ok || resolved != "https://files.example/base/p/video.mkv?sign=value" {
		t.Fatalf("path-relative OpenList URL resolution failed: %q, %v", resolved, ok)
	}
	if _, ok := normalizeRedirectURL("//evil.example/video.mkv", "https://files.example"); ok {
		t.Fatal("scheme-relative redirect escaped the trusted OpenList host")
	}
}

func TestErrorTrackingReaderCapturesUpstreamFailure(t *testing.T) {
	upstreamErr := errors.New("upstream reset")
	reader := &errorTrackingReader{reader: io.MultiReader(strings.NewReader("data"), failingReader{err: upstreamErr})}
	data, err := io.ReadAll(reader)
	if string(data) != "data" || !errors.Is(err, upstreamErr) || !errors.Is(reader.err, upstreamErr) {
		t.Fatalf("data=%q err=%v tracked=%v", data, err, reader.err)
	}
}

type failingReader struct{ err error }

func (reader failingReader) Read([]byte) (int, error) {
	return 0, reader.err
}

type handlerRoundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip handlerRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestProxyWebDAVDownloadDoesNotSuppressUpstreamReset(t *testing.T) {
	account := models.Account{Type: models.AccountTypeWebDAV, WebDAVURL: "https://dav.example"}
	account.ID = 1
	client := webdav.NewClient(account)
	client.HTTPClient = &http.Client{Transport: handlerRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, syscall.ECONNRESET
	})}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/stream", nil)
	proxyWebDAVDownload(ctx, client, "/video.mkv")
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
}

func TestWebDAVDirectLinkUsesOpenListAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	var getPath string
	openListServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]string{"token": "token"}})
		case "/api/fs/get":
			if r.Header.Get("Authorization") != "token" {
				t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
			}
			var body struct {
				Path string `json:"path"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			getPath = body.Path
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200,
				"data": map[string]any{"raw_url": "https://storage.example/video.mkv?sign=value"},
			})
		default:
			http.Error(w, "unexpected media proxy request", http.StatusInternalServerError)
		}
	}))
	defer openListServer.Close()

	account := models.Account{
		Name:             "openlist-dav",
		Type:             models.AccountTypeWebDAV,
		WebDAVURL:        openListServer.URL + "/dav",
		WebDAVUsername:   "user",
		WebDAVPassword:   "secret",
		WebDAVDirectLink: true,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	defer openlist.InvalidateAccountCache(account.ID)

	router := gin.New()
	router.GET("/api/v1/stream/s/*path", UnifiedStreamHandler)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/stream/s/1/media/video.mkv", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if location := recorder.Header().Get("Location"); location != "https://storage.example/video.mkv?sign=value" {
		t.Fatalf("Location = %q", location)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" || recorder.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("redirect safety headers missing: %v", recorder.Header())
	}
	if getPath != "/media/video.mkv" {
		t.Fatalf("OpenList path = %q", getPath)
	}
}

func TestWebDAVDirectLinkResolvesRelativeOpenListURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	openListServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]string{"token": "token"}})
		case "/api/fs/get":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200,
				"data": map[string]any{"raw_url": "/p/media/video.mkv?sign=value"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer openListServer.Close()

	account := models.Account{
		Name:             "relative-dav",
		Type:             models.AccountTypeWebDAV,
		WebDAVURL:        openListServer.URL + "/dav",
		WebDAVUsername:   "user",
		WebDAVPassword:   "secret",
		WebDAVDirectLink: true,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	defer openlist.InvalidateAccountCache(account.ID)

	router := gin.New()
	router.GET("/api/v1/stream/s/*path", UnifiedStreamHandler)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/stream/s/1/media/video.mkv", nil))

	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if location := recorder.Header().Get("Location"); location != openListServer.URL+"/p/media/video.mkv?sign=value" {
		t.Fatalf("Location = %q", location)
	}
}

func TestSignedWebDAVDirectLinkSupportsHead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	openListServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]string{"token": "token"}})
		case "/api/fs/get":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200,
				"data": map[string]any{"raw_url": "https://storage.example/video.mkv?sign=value"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer openListServer.Close()

	account := models.Account{
		Name:             "signed-direct-dav",
		Type:             models.AccountTypeWebDAV,
		WebDAVURL:        openListServer.URL + "/dav",
		WebDAVUsername:   "user",
		WebDAVPassword:   "secret",
		WebDAVDirectLink: true,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	defer openlist.InvalidateAccountCache(account.ID)
	task := models.Task{
		Name:           "signed-task",
		AccountID:      account.ID,
		SourceFolderID: "/",
		LocalPath:      t.TempDir(),
		Cron:           "0 * * * *",
		Enabled:        true,
		EncodePath:     true,
		Threads:        1,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	sign, err := auth.SignStreamURL(task.ID, account.ID, "/media/video.mkv", 1)
	if err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.Match([]string{http.MethodGet, http.MethodHead}, "/api/v1/stream/s/*path", UnifiedStreamHandler)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodHead, "/api/v1/stream/s/placeholder?sign="+url.QueryEscape(sign), nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "https://storage.example/video.mkv?sign=value" {
		t.Fatalf("status=%d location=%q", recorder.Code, recorder.Header().Get("Location"))
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("HEAD response body = %q", recorder.Body.String())
	}
}

func TestGetStreamClientDoesNotCacheAccountConfiguration(t *testing.T) {
	account := models.Account{Type: models.AccountTypeWebDAV, WebDAVURL: "https://old.example", WebDAVPassword: "old"}
	first := getStreamClient(account).(*cachedWebDAVClient)
	account.WebDAVURL = "https://new.example"
	account.WebDAVPassword = "new"
	second := getStreamClient(account).(*cachedWebDAVClient)

	if first.Client == second.Client {
		t.Fatal("stream client was cached")
	}
	if second.BaseURL != "https://new.example" || second.Password != "new" {
		t.Fatalf("new configuration was not used: URL=%q password=%q", second.BaseURL, second.Password)
	}
}

func TestProxyWebDAVDownloadResponseHeaderAllowlist(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=0-3" {
			t.Errorf("Range header = %q", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Range", "bytes 0-3/4")
		w.Header().Set("Set-Cookie", "secret=value")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("data"))
	}))
	defer upstream.Close()

	account := models.Account{Type: models.AccountTypeWebDAV, WebDAVURL: upstream.URL}
	account.ID = 1
	client := webdav.NewClient(account)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/stream", nil)
	ctx.Request.Header.Set("Range", "bytes=0-3")
	proxyWebDAVDownload(ctx, client, "/video.mp4")

	if recorder.Code != http.StatusPartialContent || recorder.Body.String() != "data" {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Content-Type") != "video/mp4" || recorder.Header().Get("Content-Range") != "bytes 0-3/4" {
		t.Fatalf("media headers missing: %v", recorder.Header())
	}
	if recorder.Header().Get("Set-Cookie") != "" || recorder.Header().Get("Connection") != "" {
		t.Fatalf("unsafe headers forwarded: %v", recorder.Header())
	}
}

func TestProxyWebDAVDownloadMapsUpstreamErrorsToBadGateway(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal detail", http.StatusNotFound)
	}))
	defer upstream.Close()

	account := models.Account{Type: models.AccountTypeWebDAV, WebDAVURL: upstream.URL}
	account.ID = 1
	client := webdav.NewClient(account)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/stream", nil)
	proxyWebDAVDownload(ctx, client, "/missing.mp4")

	if recorder.Code != http.StatusBadGateway || recorder.Body.String() != "Upstream service unavailable" {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}
