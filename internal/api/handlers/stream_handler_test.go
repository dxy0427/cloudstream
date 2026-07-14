package handlers

import (
	"cloudstream/internal/models"
	"cloudstream/internal/webdav"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

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
