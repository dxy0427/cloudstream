package handlers

import (
	"cloudstream/internal/auth"
	"cloudstream/internal/models"
	"cloudstream/internal/webdav"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
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

func TestProxyStreamURLForwardsMediaHeadersAndFiltersCredentials(t *testing.T) {
	var receivedAuthorization string
	var receivedCookie string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthorization = r.Header.Get("Authorization")
		receivedCookie = r.Header.Get("Cookie")
		if r.Header.Get("Range") != "bytes=0-3" {
			t.Errorf("Range = %q", r.Header.Get("Range"))
		}
		if r.Header.Get("If-Match") != `"etag"` {
			t.Errorf("If-Match = %q", r.Header.Get("If-Match"))
		}
		if r.Header.Get("Accept-Encoding") != "identity" {
			t.Errorf("Accept-Encoding = %q", r.Header.Get("Accept-Encoding"))
		}
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Range", "bytes 0-3/4")
		w.Header().Set("Set-Cookie", "upstream=secret")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("data"))
	}))
	defer upstream.Close()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/stream", nil)
	ctx.Request.Header.Set("Range", "bytes=0-3")
	ctx.Request.Header.Set("If-Match", `"etag"`)
	ctx.Request.Header.Set("Authorization", "Bearer client-secret")
	ctx.Request.Header.Set("Cookie", "session=secret")
	proxyStreamURL(ctx, 1, upstream.URL+"/video.mp4")

	if recorder.Code != http.StatusPartialContent || recorder.Body.String() != "data" {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if receivedAuthorization != "" || receivedCookie != "" {
		t.Fatalf("credentials leaked: Authorization=%q Cookie=%q", receivedAuthorization, receivedCookie)
	}
	if recorder.Header().Get("Content-Type") != "video/mp4" || recorder.Header().Get("Content-Range") != "bytes 0-3/4" {
		t.Fatalf("media headers missing: %v", recorder.Header())
	}
	if recorder.Header().Get("Set-Cookie") != "" || recorder.Header().Get("Connection") != "" {
		t.Fatalf("unsafe response headers forwarded: %v", recorder.Header())
	}
}

func TestProxyStreamURLFollowsRedirectAndSupportsHead(t *testing.T) {
	var targetMethod string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetMethod = r.Method
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", "42")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/video.mp4", http.StatusSeeOther)
	}))
	defer source.Close()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodHead, "/stream", nil)
	proxyStreamURL(ctx, 1, source.URL+"/start")

	if recorder.Code != http.StatusOK || targetMethod != http.MethodHead {
		t.Fatalf("status=%d targetMethod=%q", recorder.Code, targetMethod)
	}
	if recorder.Body.Len() != 0 || recorder.Header().Get("Content-Length") != "42" {
		t.Fatalf("HEAD body=%q headers=%v", recorder.Body.String(), recorder.Header())
	}
}

func TestProxyStreamURLPreservesConditionalAndRangeStatuses(t *testing.T) {
	for _, test := range []struct {
		name            string
		status          int
		responseHeaders http.Header
		wantHeader      string
		wantValue       string
		forbiddenHeader string
		forbiddenValue  string
	}{
		{
			name: "not modified", status: http.StatusNotModified,
			responseHeaders: http.Header{"ETag": []string{`"etag"`}, "Content-Length": []string{"9"}},
			wantHeader:      "ETag", wantValue: `"etag"`, forbiddenHeader: "Content-Length", forbiddenValue: "9",
		},
		{
			name: "range not satisfiable", status: http.StatusRequestedRangeNotSatisfiable,
			responseHeaders: http.Header{"Content-Range": []string{"bytes */42"}, "Content-Length": []string{"9"}},
			wantHeader:      "Content-Range", wantValue: "bytes */42", forbiddenHeader: "Content-Length", forbiddenValue: "9",
		},
		{
			name: "precondition failed", status: http.StatusPreconditionFailed,
			responseHeaders: http.Header{"ETag": []string{`"new-etag"`}, "Content-Length": []string{"9"}},
			wantHeader:      "ETag", wantValue: `"new-etag"`, forbiddenHeader: "Content-Length", forbiddenValue: "9",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for key, values := range test.responseHeaders {
					for _, value := range values {
						w.Header().Add(key, value)
					}
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte("sensitive"))
			}))
			defer upstream.Close()

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/stream", nil)
			proxyStreamURL(ctx, 1, upstream.URL)
			if recorder.Code != test.status || recorder.Header().Get(test.wantHeader) != test.wantValue {
				t.Fatalf("status=%d headers=%v", recorder.Code, recorder.Header())
			}
			if recorder.Body.Len() != 0 || recorder.Header().Get(test.forbiddenHeader) == test.forbiddenValue {
				t.Fatalf("body=%q headers=%v", recorder.Body.String(), recorder.Header())
			}
		})
	}
}

func TestProxyStreamURLMapsUpstreamErrorsAndRejectsUnsafeURL(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "private upstream detail", http.StatusUnauthorized)
	}))
	defer upstream.Close()

	for _, streamURL := range []string{upstream.URL, "https://user:password@example.test/video.mp4"} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/stream", nil)
		proxyStreamURL(ctx, 1, streamURL)
		if recorder.Code != http.StatusBadGateway || recorder.Body.String() != "Upstream service unavailable" {
			t.Fatalf("URL=%q status=%d body=%q", streamURL, recorder.Code, recorder.Body.String())
		}
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

func TestWebDAVRedirectModePassesThroughUpstreamRedirect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "user" || password != "secret" {
			w.Header().Set("WWW-Authenticate", `Basic realm="DAV"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Range") != "bytes=0-1023" {
			t.Errorf("Range = %q", r.Header.Get("Range"))
		}
		http.Redirect(w, r, "https://storage.example/video.mkv?sign=value", http.StatusFound)
	}))
	defer upstream.Close()

	account := models.Account{
		Name:           "redirect-dav",
		Type:           models.AccountTypeWebDAV,
		WebDAVURL:      upstream.URL,
		WebDAVUsername: "user",
		WebDAVPassword: "secret",
		PlaybackMode:   models.PlaybackModeRedirect,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.GET("/api/v1/stream/s/*path", UnifiedStreamHandler)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/stream/s/1/media/video.mkv", nil)
	request.Header.Set("Range", "bytes=0-1023")
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
}

func TestWebDAVRedirectModeRejectsProxiedFileResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("media-data"))
	}))
	defer upstream.Close()

	account := models.Account{
		Name:         "proxy-policy-dav",
		Type:         models.AccountTypeWebDAV,
		WebDAVURL:    upstream.URL,
		PlaybackMode: models.PlaybackModeRedirect,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.GET("/api/v1/stream/s/*path", UnifiedStreamHandler)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/stream/s/1/media/video.mkv", nil))

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "media-data") {
		t.Fatalf("redirect mode unexpectedly proxied media body: %q", recorder.Body.String())
	}
}

func TestWebDAVModeIsAuthoritativeAndUnknownFallsBackToProxy(t *testing.T) {
	for _, test := range []struct {
		name string
		mode string
	}{
		{name: "proxy", mode: models.PlaybackModeProxy},
		{name: "unknown", mode: "unknown-mode"},
	} {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			db := openHandlerTestDB(t)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("media-data"))
			}))
			defer upstream.Close()

			account := models.Account{
				Name:         "mode-authority-" + test.name,
				Type:         models.AccountTypeWebDAV,
				WebDAVURL:    upstream.URL,
				PlaybackMode: test.mode,
			}
			if err := db.Create(&account).Error; err != nil {
				t.Fatal(err)
			}

			router := gin.New()
			router.GET("/api/v1/stream/s/*path", UnifiedStreamHandler)
			recorder := httptest.NewRecorder()
			path := "/api/v1/stream/s/" + strconv.FormatUint(uint64(account.ID), 10) + "/media/video.mkv"
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))

			if recorder.Code != http.StatusOK || recorder.Body.String() != "media-data" {
				t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestAccountSignedWebDAVRedirectModeSupportsHead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("method = %s", r.Method)
		}
		http.Redirect(w, r, "https://storage.example/video.mkv?sign=value", http.StatusTemporaryRedirect)
	}))
	defer upstream.Close()

	account := models.Account{
		Name:             "signed-direct-dav",
		Type:             models.AccountTypeWebDAV,
		WebDAVURL:        upstream.URL,
		PlaybackMode:     models.PlaybackModeRedirect,
		EnableStreamSign: true,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	sign, err := auth.SignAccountStreamURL(account.ID, "/media/video.mkv", 1)
	if err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.Match([]string{http.MethodGet, http.MethodHead}, "/api/v1/stream/s/*path", UnifiedStreamHandler)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodHead, "/api/v1/stream/s/placeholder?sign="+url.QueryEscape(sign), nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusTemporaryRedirect || recorder.Header().Get("Location") != "https://storage.example/video.mkv?sign=value" {
		t.Fatalf("status=%d location=%q", recorder.Code, recorder.Header().Get("Location"))
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("HEAD response body = %q", recorder.Body.String())
	}
}

func TestAccountSignedStreamRequiresSigningToRemainEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{
		Name:             "disabled-sign-dav",
		Type:             models.AccountTypeWebDAV,
		WebDAVURL:        "https://dav.example",
		PlaybackMode:     models.PlaybackModeProxy,
		EnableStreamSign: false,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	sign, err := auth.SignAccountStreamURL(account.ID, "/media/video.mkv", 1)
	if err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.GET("/api/v1/stream/s/*path", UnifiedStreamHandler)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/stream/s/placeholder?sign="+url.QueryEscape(sign), nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestWebDAVRedirectModePassesNotModifiedAndRangeErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		header string
		value  string
	}{
		{name: "not-modified", status: http.StatusNotModified, header: "ETag", value: `"etag"`},
		{name: "range-error", status: http.StatusRequestedRangeNotSatisfiable, header: "Content-Range", value: "bytes */42"},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(test.header, test.value)
				w.WriteHeader(test.status)
			}))
			defer upstream.Close()
			account := models.Account{Type: models.AccountTypeWebDAV, WebDAVURL: upstream.URL, PlaybackMode: models.PlaybackModeRedirect}
			client := webdav.NewClient(account)
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/stream", nil)
			redirectWebDAVDownload(ctx, client, "/video.mkv")
			if recorder.Code != test.status || recorder.Header().Get(test.header) != test.value {
				t.Fatalf("status=%d headers=%v", recorder.Code, recorder.Header())
			}
		})
	}
}

func TestWebDAVProxyStripsAuthorizationAcrossOrigins(t *testing.T) {
	var receivedAuthorization string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthorization = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data"))
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if username, password, ok := r.BasicAuth(); !ok || username != "user" || password != "secret" {
			w.Header().Set("WWW-Authenticate", `Basic realm="DAV"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		http.Redirect(w, r, target.URL+"/video.mkv", http.StatusFound)
	}))
	defer source.Close()
	account := models.Account{Type: models.AccountTypeWebDAV, WebDAVURL: source.URL, WebDAVUsername: "user", WebDAVPassword: "secret"}
	client := webdav.NewClient(account)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/stream", nil)
	proxyWebDAVDownload(ctx, client, "/video.mkv")
	if recorder.Code != http.StatusOK || recorder.Body.String() != "data" {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if receivedAuthorization != "" {
		t.Fatalf("Authorization leaked to redirect target: %q", receivedAuthorization)
	}
}

func TestWebDAVProxyStripsAuthorizationOnExactOriginRedirect(t *testing.T) {
	var receivedAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/video.mkv" {
			if _, _, ok := r.BasicAuth(); !ok {
				w.Header().Set("WWW-Authenticate", `Basic realm="DAV"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/storage/video.mkv", http.StatusFound)
			return
		}
		receivedAuthorization = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data"))
	}))
	defer server.Close()
	account := models.Account{Type: models.AccountTypeWebDAV, WebDAVURL: server.URL, WebDAVUsername: "user", WebDAVPassword: "secret"}
	client := webdav.NewClient(account)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/stream", nil)
	proxyWebDAVDownload(ctx, client, "/video.mkv")
	if recorder.Code != http.StatusOK || receivedAuthorization != "" {
		t.Fatalf("status=%d Authorization=%q", recorder.Code, receivedAuthorization)
	}
}

func TestWebDAVProxyDoesNotAnswerRedirectTargetAuthChallenge(t *testing.T) {
	var challengedRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		challengedRequests.Add(1)
		w.Header().Set("WWW-Authenticate", `Basic realm="target"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := r.BasicAuth(); !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="source"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		http.Redirect(w, r, target.URL+"/video.mkv", http.StatusFound)
	}))
	defer source.Close()
	client := webdav.NewClient(models.Account{Type: models.AccountTypeWebDAV, WebDAVURL: source.URL, WebDAVUsername: "user", WebDAVPassword: "secret"})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/stream", nil)
	proxyWebDAVDownload(ctx, client, "/video.mkv")
	if recorder.Code != http.StatusBadGateway || challengedRequests.Load() != 1 {
		t.Fatalf("status=%d target requests=%d", recorder.Code, challengedRequests.Load())
	}
}

func TestGetStreamClientDoesNotCacheAccountConfiguration(t *testing.T) {
	account := models.Account{Type: models.AccountTypeWebDAV, WebDAVURL: "https://old.example", WebDAVPassword: "old"}
	first := getStreamClient(account).(*webdav.Client)
	account.WebDAVURL = "https://new.example"
	account.WebDAVPassword = "new"
	second := getStreamClient(account).(*webdav.Client)

	if first == second {
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

func TestProxyWebDAVDownloadPreservesRangeNotSatisfiable(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes */42")
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
	}))
	defer upstream.Close()
	client := webdav.NewClient(models.Account{Type: models.AccountTypeWebDAV, WebDAVURL: upstream.URL})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/stream", nil)
	proxyWebDAVDownload(ctx, client, "/video.mp4")
	if recorder.Code != http.StatusRequestedRangeNotSatisfiable || recorder.Header().Get("Content-Range") != "bytes */42" {
		t.Fatalf("status=%d headers=%v", recorder.Code, recorder.Header())
	}
}

func TestFollowWebDAVRedirectsKeepsHeadForSeeOther(t *testing.T) {
	var targetMethod string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	response := &http.Response{
		StatusCode: http.StatusSeeOther,
		Header:     http.Header{"Location": []string{target.URL}},
		Body:       io.NopCloser(strings.NewReader("")),
	}
	client := noRedirectHTTPClient(target.Client())
	final, err := followStreamRedirects(context.Background(), client, response, http.MethodHead, nil)
	if err != nil {
		t.Fatal(err)
	}
	final.Body.Close()
	if targetMethod != http.MethodHead {
		t.Fatalf("target method = %s", targetMethod)
	}
}
