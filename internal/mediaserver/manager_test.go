package mediaserver

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type recordingMediaClient struct {
	info      MediaItemInfo
	infoByID  map[string]MediaItemInfo
	calls     atomic.Int32
	itemID    string
	sourceID  string
	accessKey string
}

func (c *recordingMediaClient) GetItemInfo(itemID, sourceID, accessKey string) (MediaItemInfo, error) {
	c.calls.Add(1)
	c.itemID = itemID
	c.sourceID = sourceID
	c.accessKey = accessKey
	if info, ok := c.infoByID[sourceID]; ok {
		return info, nil
	}
	return c.info, nil
}

func (c *recordingMediaClient) Ping() error { return nil }

func TestReloadAllSkipsInvalidServer(t *testing.T) {
	previousDB := database.DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "media.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.MediaServer{}); err != nil {
		t.Fatal(err)
	}
	database.DB = db
	t.Cleanup(func() { database.DB = previousDB })

	valid := models.MediaServer{Name: "valid", ServerType: "Emby", ServerAddr: "https://example.com", APIKey: "key", Enabled: true}
	invalid := models.MediaServer{Name: "invalid", ServerType: "Emby", ServerAddr: "://bad", APIKey: "key", Enabled: true}
	if err := db.Create(&valid).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&invalid).Error; err != nil {
		t.Fatal(err)
	}

	manager := &Manager{servers: make(map[uint]*ProxyServer)}
	defer manager.CloseAll()
	if err := manager.ReloadAll(); err == nil {
		t.Fatal("expected invalid-server warning error")
	}
	if _, ok := manager.GetServer(valid.ID); !ok {
		t.Fatal("valid server was not loaded")
	}
	if _, ok := manager.GetServer(invalid.ID); ok {
		t.Fatal("invalid server was loaded")
	}
}

func TestValidateServerAddress(t *testing.T) {
	if err := ValidateServerAddress("https://example.com/emby"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateServerAddress("://bad"); err == nil {
		t.Fatal("expected invalid address")
	}
}

func TestProxyRetireWaitsForActiveRequest(t *testing.T) {
	proxy := NewProxyServer(&Config{Server: ServerConf{Addr: "https://example.com"}})
	if !proxy.beginRequest() {
		t.Fatal("failed to acquire initial request lease")
	}
	done := proxy.Retire()
	select {
	case <-done:
		t.Fatal("proxy retired before active request ended")
	case <-time.After(20 * time.Millisecond):
	}
	if proxy.beginRequest() {
		t.Fatal("retired proxy accepted a new request")
	}
	proxy.endRequest()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("proxy did not finish retiring")
	}
}

func TestHTTPStrmUsesCallerTokenAndGETResolver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var resolverMethod string
	resolver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resolverMethod = r.Method
		switch r.URL.Path {
		case "/video.mp4":
			http.Redirect(w, r, "/middle.mp4", http.StatusFound)
			return
		case "/middle.mp4":
			http.Redirect(w, r, "/final.mp4", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer resolver.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("stream request unexpectedly fell back to the media server")
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer upstream.Close()

	client := &recordingMediaClient{info: MediaItemInfo{ItemPath: "/media/video.strm", MediaPath: "http://source.invalid/video.mp4", Protocol: "Http"}}
	proxy := NewProxyServer(&Config{
		Server: ServerConf{Addr: upstream.URL, Auth: "configured-admin-key"},
		HttpStrm: HttpStrmConf{
			Enable: true, ResolveStrmLinks: true,
			PathMappings: []PathMapping{{Old: "http://source.invalid", New: resolver.URL}},
		},
	})
	defer proxy.Close()
	proxy.mediaServer = client
	manager := &Manager{servers: map[uint]*ProxyServer{1: proxy}}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/videos/item-42/stream?MediaSourceId=source-1", nil)
	ctx.Request.Header.Set("X-Emby-Token", "caller-token")
	manager.HandleProxy(ctx, 1, "/videos/item-42/stream")

	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != resolver.URL+"/final.mp4" {
		t.Fatalf("status=%d location=%q", recorder.Code, recorder.Header().Get("Location"))
	}
	if resolverMethod != http.MethodGet {
		t.Fatalf("resolver method=%q", resolverMethod)
	}
	if client.calls.Load() != 1 || client.itemID != "item-42" || client.sourceID != "source-1" || client.accessKey != "caller-token" {
		t.Fatalf("item calls=%d item=%q source=%q token=%q", client.calls.Load(), client.itemID, client.sourceID, client.accessKey)
	}
}

func TestHTTPStrmHEADUsesHEADResolver(t *testing.T) {
	var resolverMethod string
	resolver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resolverMethod = r.Method
		if r.URL.Path == "/video.mp4" {
			http.Redirect(w, r, "/final.mp4", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer resolver.Close()
	proxy := NewProxyServer(&Config{Server: ServerConf{Addr: "https://example.com"}, HttpStrm: HttpStrmConf{ResolveStrmLinks: true}})
	defer proxy.Close()

	resolved := proxy.resolveHTTPStrm(context.Background(), "head-resolver", resolver.URL+"/video.mp4", http.MethodHead, "test")
	if resolved != resolver.URL+"/final.mp4" {
		t.Fatalf("resolved URL=%q", resolved)
	}
	if resolverMethod != http.MethodHead {
		t.Fatalf("resolver method=%q", resolverMethod)
	}
}

func TestHTTPStrmCacheSeparatesGETAndHEAD(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var calls atomic.Int32
	resolver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/video.mp4" {
			http.Redirect(w, r, "/"+strings.ToLower(r.Method)+".mp4", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer resolver.Close()
	client := &recordingMediaClient{info: MediaItemInfo{ItemPath: "/media/video.strm", MediaPath: resolver.URL + "/video.mp4", Protocol: "Http"}}
	proxy := NewProxyServer(&Config{
		Server:   ServerConf{Addr: "https://example.com"},
		Cache:    CacheConf{Enable: true, HttpStrmTTL: 1},
		HttpStrm: HttpStrmConf{Enable: true, ResolveStrmLinks: true},
	})
	defer proxy.Close()
	proxy.mediaServer = client
	manager := &Manager{servers: map[uint]*ProxyServer{1: proxy}}
	router := gin.New()
	router.Any("/*path", func(c *gin.Context) {
		manager.HandleProxy(c, 1, c.Request.URL.EscapedPath())
	})
	server := httptest.NewServer(router)
	defer server.Close()
	httpClient := *server.Client()
	httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}

	for _, method := range []string{http.MethodHead, http.MethodGet, http.MethodGet} {
		request, err := http.NewRequest(method, server.URL+"/videos/item-42/stream", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("X-Emby-Token", "caller-token")
		response, err := httpClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		wantLocation := resolver.URL + "/" + strings.ToLower(method) + ".mp4"
		if response.StatusCode != http.StatusFound || response.Header.Get("Location") != wantLocation {
			t.Fatalf("method=%s status=%d location=%q", method, response.StatusCode, response.Header.Get("Location"))
		}
	}
	if calls.Load() != 4 {
		t.Fatalf("resolver calls=%d", calls.Load())
	}
}

func TestRemoteHTTPMediaThatIsNotSTRMFallsBackToUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	client := &recordingMediaClient{info: MediaItemInfo{ItemPath: "/media/video.mkv", MediaPath: "https://remote.example/video.mkv", Protocol: "Http"}}
	proxy := NewProxyServer(&Config{Server: ServerConf{Addr: upstream.URL}, HttpStrm: HttpStrmConf{Enable: true}})
	defer proxy.Close()
	proxy.mediaServer = client
	manager := &Manager{servers: map[uint]*ProxyServer{1: proxy}}

	status := requestManagerProxy(t, manager, "/videos/item-42/stream", "caller-token")
	if status != http.StatusNoContent || client.calls.Load() != 1 {
		t.Fatalf("status=%d itemCalls=%d", status, client.calls.Load())
	}
}

func TestHTTPStrmDoesNotUseConfiguredKeyForAnonymousRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var upstreamToken string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamToken = r.Header.Get("X-Emby-Token")
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer upstream.Close()
	client := &recordingMediaClient{info: MediaItemInfo{ItemPath: "/media/video.strm", MediaPath: "https://storage.example/video.mp4", Protocol: "Http"}}
	proxy := NewProxyServer(&Config{Server: ServerConf{Addr: upstream.URL, Auth: "configured-admin-key"}, HttpStrm: HttpStrmConf{Enable: true}})
	defer proxy.Close()
	proxy.mediaServer = client
	manager := &Manager{servers: map[uint]*ProxyServer{1: proxy}}

	status := requestManagerProxy(t, manager, "/videos/item-42/stream", "")
	if status != http.StatusUnauthorized || client.calls.Load() != 0 || upstreamToken != "" {
		t.Fatalf("status=%d itemCalls=%d upstreamToken=%q", status, client.calls.Load(), upstreamToken)
	}
}

func TestHTTPStrmDisabledOrMissingItemSkipsItemLookup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name    string
		enabled bool
		path    string
	}{
		{name: "disabled", enabled: false, path: "/videos/item-42/stream"},
		{name: "missing item", enabled: true, path: "/stream"},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			defer upstream.Close()
			client := &recordingMediaClient{info: MediaItemInfo{ItemPath: "/media/video.strm", MediaPath: "https://storage.example/video.mp4", Protocol: "Http"}}
			proxy := NewProxyServer(&Config{Server: ServerConf{Addr: upstream.URL}, HttpStrm: HttpStrmConf{Enable: test.enabled}})
			defer proxy.Close()
			proxy.mediaServer = client
			manager := &Manager{servers: map[uint]*ProxyServer{1: proxy}}

			status := requestManagerProxy(t, manager, test.path+"?api_key=caller", "")
			if status != http.StatusNoContent || client.calls.Load() != 0 {
				t.Fatalf("status=%d itemCalls=%d", status, client.calls.Load())
			}
		})
	}
}

func requestManagerProxy(t *testing.T, manager *Manager, requestPath, token string) int {
	t.Helper()
	router := gin.New()
	router.Any("/*path", func(c *gin.Context) {
		manager.HandleProxy(c, 1, c.Request.URL.EscapedPath())
	})
	server := httptest.NewServer(router)
	defer server.Close()
	request, err := http.NewRequest(http.MethodGet, server.URL+requestPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("X-Emby-Token", token)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode
}

func TestPathMappingUsesFirstURLPrefixMatch(t *testing.T) {
	mappings := []PathMapping{
		{Old: "http://127.0.0.1:5244", New: "https://list.example"},
		{Old: "https://list.example", New: "https://must-not-chain.example"},
	}
	if got := applyPathMappings("http://127.0.0.1:5244/d/video.mp4", mappings); got != "https://list.example/d/video.mp4" {
		t.Fatalf("mapped URL=%q", got)
	}
	if got := applyPathMappings("http://127.0.0.1:52440/d/video.mp4", mappings); got != "http://127.0.0.1:52440/d/video.mp4" {
		t.Fatalf("mapping crossed host boundary: %q", got)
	}
}

func TestRequestAccessTokenSupportsEmbyAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	ctx.Request.Header.Set("X-Emby-Authorization", `MediaBrowser Client="Emby Web", Device="Browser", Token="caller-token"`)
	if token := requestAccessToken(ctx); token != "caller-token" {
		t.Fatalf("token=%q", token)
	}
}

func TestPlaybackInfoModificationOnlyDisablesHTTPStrmTranscoding(t *testing.T) {
	proxy := NewProxyServer(&Config{Server: ServerConf{Addr: "https://example.com"}, HttpStrm: HttpStrmConf{Enable: true, DisableTranscode: true}})
	defer proxy.Close()
	proxy.mediaServer = &recordingMediaClient{infoByID: map[string]MediaItemInfo{
		"http-source":  {ItemPath: "/media/http-video.strm", MediaPath: "https://list.example/video.mkv", Protocol: "Http"},
		"local-source": {ItemPath: "/media/video.mkv", MediaPath: "https://remote.example/video.mkv", Protocol: "Http"},
	}}
	body := `{"MediaSources":[` +
		`{"Id":"http-source","ItemId":"1","Path":"https://list.example/video.mkv","DirectStreamUrl":"/Videos/1/master.m3u8?api_key=caller-token&x=1","SupportsDirectPlay":false,"SupportsDirectStream":false,"SupportsTranscoding":true,"TranscodingUrl":"/Videos/1/master.m3u8","TranscodingContainer":"ts","TranscodingSubProtocol":"hls"},` +
		`{"Id":"local-source","ItemId":"2","Path":"/media/video.mkv","DirectStreamUrl":"/Videos/2/master.m3u8","SupportsDirectPlay":false,"SupportsDirectStream":false,"SupportsTranscoding":true,"TranscodingUrl":"/Videos/2/master.m3u8"}` +
		`]}`
	request := httptest.NewRequest(http.MethodPost, "/Items/1/PlaybackInfo", nil)
	request = request.WithContext(context.WithValue(request.Context(), proxyRequestContextKey{}, proxyRequestOptions{modifyPlaybackInfo: true, playbackItemID: "1", playbackToken: "caller-token"}))
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"ETag": []string{`"old"`}, "Content-MD5": []string{"old"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
	if err := proxy.modifyProxyResponse(response); err != nil {
		t.Fatal(err)
	}
	modified, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		MediaSources []struct {
			ID                     string `json:"Id"`
			DirectStreamURL        string `json:"DirectStreamUrl"`
			SupportsDirectPlay     bool   `json:"SupportsDirectPlay"`
			SupportsDirectStream   bool   `json:"SupportsDirectStream"`
			SupportsTranscoding    bool   `json:"SupportsTranscoding"`
			TranscodingURL         string `json:"TranscodingUrl"`
			TranscodingContainer   string `json:"TranscodingContainer"`
			TranscodingSubProtocol string `json:"TranscodingSubProtocol"`
		} `json:"MediaSources"`
	}
	if err := json.Unmarshal(modified, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.MediaSources) != 2 {
		t.Fatalf("media sources=%d", len(result.MediaSources))
	}
	httpSource := result.MediaSources[0]
	if !httpSource.SupportsDirectPlay || httpSource.SupportsDirectStream || httpSource.SupportsTranscoding ||
		httpSource.TranscodingURL != "" || httpSource.TranscodingContainer != "" || httpSource.TranscodingSubProtocol != "" ||
		httpSource.DirectStreamURL != "/Videos/1/stream?MediaSourceId=http-source&Static=true&api_key=caller-token" {
		t.Fatalf("HTTPStrm source=%+v", httpSource)
	}
	localSource := result.MediaSources[1]
	if localSource.SupportsDirectPlay || localSource.SupportsDirectStream || !localSource.SupportsTranscoding ||
		localSource.TranscodingURL != "/Videos/2/master.m3u8" || localSource.DirectStreamURL != "/Videos/2/master.m3u8" {
		t.Fatalf("local source was modified: %+v", localSource)
	}
	if response.Header.Get("ETag") != "" || response.Header.Get("Content-MD5") != "" || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("unsafe response headers=%v", response.Header)
	}
}

func TestPlaybackInfoHTTPStrmCanKeepTranscodingEnabled(t *testing.T) {
	proxy := NewProxyServer(&Config{Server: ServerConf{Addr: "https://example.com"}, HttpStrm: HttpStrmConf{Enable: true, DisableTranscode: false}})
	defer proxy.Close()
	proxy.mediaServer = &recordingMediaClient{info: MediaItemInfo{ItemPath: "/media/video.strm", MediaPath: "https://list.example/video.mkv", Protocol: "Http"}}
	body := []byte(`{"MediaSources":[{"Id":"http-source","ItemId":"1","Path":"https://list.example/video.mkv","DirectStreamUrl":"/Videos/1/master.m3u8?api_key=caller-token","SupportsDirectPlay":false,"SupportsTranscoding":true,"TranscodingUrl":"/Videos/1/master.m3u8"}]}`)
	modified := proxy.modifyPlaybackInfo(body, "1", "caller-token")
	text := string(modified)
	if !strings.Contains(text, `"SupportsDirectPlay":true`) || !strings.Contains(text, `"SupportsTranscoding":true`) ||
		!strings.Contains(text, `"TranscodingUrl":"/Videos/1/master.m3u8"`) ||
		!strings.Contains(text, `"DirectStreamUrl":"/Videos/1/stream?MediaSourceId=http-source&Static=true&api_key=caller-token"`) {
		t.Fatalf("modified body=%s", text)
	}
}

func TestPlaybackInfoFallsBackToCallerTokenWhenDirectURLCredentialIsEmpty(t *testing.T) {
	proxy := NewProxyServer(&Config{Server: ServerConf{Addr: "https://example.com"}, HttpStrm: HttpStrmConf{Enable: true}})
	defer proxy.Close()
	proxy.mediaServer = &recordingMediaClient{info: MediaItemInfo{ItemPath: "/media/video.strm", MediaPath: "https://list.example/video.mkv", Protocol: "Http"}}
	body := []byte(`{"MediaSources":[{"Id":"http-source","ItemId":"1","Path":"https://list.example/video.mkv","DirectStreamUrl":"/Videos/1/master.m3u8?X-Emby-Token=","SupportsDirectPlay":false}]}`)
	modified := string(proxy.modifyPlaybackInfo(body, "1", "caller-token"))
	if !strings.Contains(modified, `"DirectStreamUrl":"/Videos/1/stream?MediaSourceId=http-source&Static=true&api_key=caller-token"`) {
		t.Fatalf("modified body=%s", modified)
	}
}

func TestPrivateNetworkTargetDetection(t *testing.T) {
	for _, rawURL := range []string{
		"http://127.0.0.1/video",
		"http://[::1]/video",
		"http://localhost/video",
		"http://169.254.169.254/latest/meta-data",
		"http://100.64.0.1/video",
		"http://198.18.0.1/video",
		"http://192.0.2.1/video",
		"http://[2001:db8::1]/video",
	} {
		target, err := url.Parse(rawURL)
		if err != nil {
			t.Fatal(err)
		}
		private, err := URLTargetsPrivateNetwork(context.Background(), target)
		if err != nil || !private {
			t.Fatalf("URL=%s private=%v error=%v", rawURL, private, err)
		}
	}
}

func TestResolveHTTPStrmDoesNotCacheFailedTerminalResponse(t *testing.T) {
	var calls atomic.Int32
	resolver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/video.mp4" {
			http.Redirect(w, r, "/expired.mp4", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer resolver.Close()
	proxy := NewProxyServer(&Config{
		Server:   ServerConf{Addr: "https://example.com"},
		Cache:    CacheConf{Enable: true, HttpStrmTTL: 1},
		HttpStrm: HttpStrmConf{ResolveStrmLinks: true},
	})
	defer proxy.Close()
	original := resolver.URL + "/video.mp4"
	for range 2 {
		if resolved := proxy.resolveHTTPStrm(context.Background(), "failed-terminal", original, http.MethodGet, "test"); resolved != original {
			t.Fatalf("resolved URL=%q", resolved)
		}
	}
	if calls.Load() != 4 {
		t.Fatalf("resolver calls=%d", calls.Load())
	}
}

func TestClientFilterIgnoresEmptyItemsAndMatchesCaseInsensitively(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	ctx.Request.Header.Set("User-Agent", "EmBy Theater")
	if allowProxyClient(ctx, ClientFilterConf{Enable: true, Mode: "BlackList", List: []string{"", "emby"}}) {
		t.Fatal("case-insensitive blacklist match was allowed")
	}
	ctx, _ = gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	if !allowProxyClient(ctx, ClientFilterConf{Enable: true, Mode: "BlackList", List: []string{""}}) {
		t.Fatal("empty blacklist item rejected every client")
	}
}

func TestResolveHTTPStrmCoalescesConcurrentMisses(t *testing.T) {
	var calls atomic.Int32
	resolver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)
		if r.URL.Path == "/video.mp4" {
			http.Redirect(w, r, "/final.mp4", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer resolver.Close()
	proxy := NewProxyServer(&Config{
		Server:   ServerConf{Addr: "https://example.com"},
		Cache:    CacheConf{Enable: true, HttpStrmTTL: 1},
		HttpStrm: HttpStrmConf{ResolveStrmLinks: true},
	})
	defer proxy.Close()

	results := make(chan string, 2)
	for range 2 {
		go func() {
			results <- proxy.resolveHTTPStrm(context.Background(), "same-key", resolver.URL+"/video.mp4", http.MethodGet, "test")
		}()
	}
	for range 2 {
		if result := <-results; result != resolver.URL+"/final.mp4" {
			t.Fatalf("resolved URL=%q", result)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("resolver calls=%d", calls.Load())
	}
}

func TestProxyRemovesCloudStreamCookieBeforeUpstream(t *testing.T) {
	var upstreamCookie string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCookie = r.Header.Get("Cookie")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	proxy := NewProxyServer(&Config{Server: ServerConf{Addr: upstream.URL}})
	defer proxy.Close()
	router := gin.New()
	router.Any("/*path", func(c *gin.Context) {
		proxy.ReverseProxy(c, c.Request.URL.EscapedPath(), false)
	})
	server := httptest.NewServer(router)
	defer server.Close()
	request, err := http.NewRequest(http.MethodGet, server.URL+"/System/Info", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Cookie", "cloudstream_token=secret; emby_session=allowed")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNoContent || upstreamCookie != "emby_session=allowed" {
		t.Fatalf("status=%d upstream cookie=%q", response.StatusCode, upstreamCookie)
	}
}

func TestWebSocketHeadersAndOriginDoNotLeakCloudStreamSession(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://cloudstream.example:8091/socket", nil)
	request.Host = "cloudstream.example:8091"
	request.Header.Set("Origin", "http://cloudstream.example:8091")
	request.Header.Set("Cookie", "cloudstream_token=secret; emby_session=allowed")
	request.Header.Set("X-Emby-Token", "emby-token")
	if !webSocketOriginAllowed(request) {
		t.Fatal("same-origin WebSocket request was rejected")
	}
	header := webSocketBackendHeaders(request)
	if header.Get("Cookie") != "emby_session=allowed" || header.Get("X-Emby-Token") != "emby-token" {
		t.Fatalf("backend headers=%v", header)
	}

	request.Header.Set("Origin", "https://evil.example")
	if webSocketOriginAllowed(request) {
		t.Fatal("cross-origin WebSocket request was allowed")
	}
}
