package mediaserver

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const maxPlaybackInfoBodySize int64 = 8 << 20

type proxyRequestContextKey struct{}

type proxyRequestOptions struct {
	target             *url.URL
	modifyPlaybackInfo bool
}

// ProxyServer 媒体服务器代理
type ProxyServer struct {
	cfg            *Config
	mediaServer    MediaServerClient
	httpClient     *http.Client
	cache          *Cache
	transport      *http.Transport
	reverseProxy   *httputil.ReverseProxy
	remoteURL      *url.URL
	configErr      error
	shutdown       chan struct{}
	requestMu      sync.Mutex
	requestClosed  bool
	requestWG      sync.WaitGroup
	retireOnce     sync.Once
	closeDone      chan struct{}
	resolveMu      sync.Mutex
	resolveFlights map[string]*resolveFlight
}

type resolveFlight struct {
	done   chan struct{}
	target string
}

// Cache 内存缓存（带大小限制）
type Cache struct {
	data      map[string]cacheEntry
	mu        sync.RWMutex
	maxSize   int
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

type cacheEntry struct {
	value     string
	expiresAt time.Time
}

func NewCache() *Cache {
	c := &Cache{
		data:    make(map[string]cacheEntry),
		maxSize: 5000,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	// 定期清理过期缓存，防止内存泄漏
	go c.cleanupLoop()
	return c
}

func (c *Cache) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	defer close(c.done)

	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			now := time.Now()
			for k, v := range c.data {
				if now.After(v.expiresAt) {
					delete(c.data, k)
				}
			}
			c.mu.Unlock()
		case <-c.stop:
			return
		}
	}
}

func (c *Cache) Close() {
	c.closeOnce.Do(func() {
		close(c.stop)
		<-c.done
	})
}

func (c *Cache) Get(key string) (string, bool) {
	c.mu.RLock()
	entry, exists := c.data[key]
	c.mu.RUnlock()

	if !exists {
		return "", false
	}
	if time.Now().After(entry.expiresAt) {
		// 过期条目由 cleanupLoop 定期清理，此处直接返回未命中
		return "", false
	}
	return entry.value, true
}

func (c *Cache) Set(key string, value string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 缓存满时清理过期条目
	if len(c.data) >= c.maxSize {
		now := time.Now()
		for k, v := range c.data {
			if now.After(v.expiresAt) {
				delete(c.data, k)
			}
		}
		// 如果清理后仍然满，跳过写入
		if len(c.data) >= c.maxSize {
			return
		}
	}

	c.data[key] = cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
}

func NewProxyServer(cfg *Config) *ProxyServer {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	s := &ProxyServer{
		cfg:            cfg,
		cache:          NewCache(),
		transport:      transport,
		shutdown:       make(chan struct{}),
		closeDone:      make(chan struct{}),
		resolveFlights: make(map[string]*resolveFlight),
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   10 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}

	remote, err := parseServerURL(cfg.Server.Addr)
	if err != nil {
		s.configErr = err
		return s
	}
	s.remoteURL = remote

	proxy := httputil.NewSingleHostReverseProxy(remote)
	baseDirector := proxy.Director
	proxy.Transport = transport
	proxy.Director = func(req *http.Request) {
		req.Header = req.Header.Clone()
		removeCloudStreamCookie(req.Header)
		baseDirector(req)

		options, ok := req.Context().Value(proxyRequestContextKey{}).(proxyRequestOptions)
		if !ok || options.target == nil {
			return
		}

		req.Host = options.target.Host
		req.URL.Scheme = options.target.Scheme
		req.URL.Host = options.target.Host
		req.URL.User = options.target.User
		req.URL.Path = options.target.Path
		req.URL.RawPath = options.target.RawPath
		req.URL.RawQuery = options.target.RawQuery
		req.URL.ForceQuery = options.target.ForceQuery
		req.URL.Fragment = ""

		if options.modifyPlaybackInfo {
			req.Header.Set("Accept-Encoding", "identity")
		}
	}
	proxy.ModifyResponse = s.modifyProxyResponse
	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		log.Error().
			Str("target", urlForLog(req.URL.String())).
			Str("error", errorWithoutURL(err)).
			Msg("媒体服务器代理请求失败")
		if req.Context().Err() == nil {
			http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
		}
	}
	s.reverseProxy = proxy

	return s
}

func removeCloudStreamCookie(header http.Header) {
	rawCookies := header.Values("Cookie")
	header.Del("Cookie")
	for _, rawCookie := range rawCookies {
		request := &http.Request{Header: http.Header{"Cookie": []string{rawCookie}}}
		cookies := make([]string, 0)
		for _, cookie := range request.Cookies() {
			if cookie.Name != "cloudstream_token" {
				cookies = append(cookies, cookie.String())
			}
		}
		if len(cookies) > 0 {
			header.Add("Cookie", strings.Join(cookies, "; "))
		}
	}
}

func (s *ProxyServer) ReloadClient() {
	serverHost := s.cfg.Server.Addr
	if remote, err := parseServerURL(serverHost); err == nil {
		clientURL := *remote
		clientURL.RawQuery = ""
		clientURL.ForceQuery = false
		clientURL.Fragment = ""
		serverHost = clientURL.String()
	} else if !strings.HasPrefix(serverHost, "http://") && !strings.HasPrefix(serverHost, "https://") {
		serverHost = "http://" + serverHost
	}

	s.mediaServer = NewClient(s.cfg.Server.Type, serverHost, s.cfg.Server.Auth)
}

func (s *ProxyServer) Close() {
	<-s.Retire()
}

func (s *ProxyServer) Retire() <-chan struct{} {
	s.retireOnce.Do(func() {
		s.requestMu.Lock()
		s.requestClosed = true
		close(s.shutdown)
		s.requestMu.Unlock()
		go func() {
			s.requestWG.Wait()
			s.cache.Close()
			s.transport.CloseIdleConnections()
			if closer, ok := s.mediaServer.(interface{ Close() }); ok {
				closer.Close()
			}
			close(s.closeDone)
		}()
	})
	return s.closeDone
}

func (s *ProxyServer) beginRequest() bool {
	s.requestMu.Lock()
	defer s.requestMu.Unlock()
	if s.requestClosed {
		return false
	}
	s.requestWG.Add(1)
	return true
}

func (s *ProxyServer) endRequest() {
	s.requestWG.Done()
}

func (s *ProxyServer) ReverseProxy(c *gin.Context, upstreamPath string, modifyResponse bool) {
	if s.configErr != nil || s.reverseProxy == nil {
		c.String(http.StatusInternalServerError, "Config Error: Invalid Server Addr")
		return
	}

	target, err := s.buildTargetURL(upstreamPath, c.Request.URL.RawQuery)
	if err != nil {
		c.String(http.StatusBadRequest, "Invalid upstream path")
		return
	}

	options := proxyRequestOptions{
		target:             target,
		modifyPlaybackInfo: modifyResponse,
	}
	ctx := context.WithValue(c.Request.Context(), proxyRequestContextKey{}, options)
	s.reverseProxy.ServeHTTP(c.Writer, c.Request.WithContext(ctx))
}

func (s *ProxyServer) buildTargetURL(upstreamPath, rawQuery string) (*url.URL, error) {
	if s.remoteURL == nil {
		return nil, fmt.Errorf("invalid server address")
	}

	path := upstreamPath
	if path == "" {
		path = "/"
	} else if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	requestURL := &url.URL{Path: path}
	if strings.Contains(path, "%") {
		decodedPath, err := url.PathUnescape(path)
		if err != nil {
			return nil, fmt.Errorf("invalid escaped path: %w", err)
		}
		requestURL.Path = decodedPath
		requestURL.RawPath = path
	}
	joinedPath, joinedEscapedPath := joinURLPath(s.remoteURL, requestURL)

	target := *s.remoteURL
	target.Path = joinedPath
	target.RawPath = joinedEscapedPath
	target.RawQuery = joinURLQuery(s.remoteURL.RawQuery, rawQuery)
	target.ForceQuery = s.remoteURL.ForceQuery
	target.Fragment = ""
	return &target, nil
}

func (s *ProxyServer) modifyProxyResponse(resp *http.Response) error {
	options, ok := resp.Request.Context().Value(proxyRequestContextKey{}).(proxyRequestOptions)
	if !ok || !options.modifyPlaybackInfo || resp.Request.Method == http.MethodHead {
		return nil
	}

	bodyBytes, err := readAndCloseLimited(resp.Body, maxPlaybackInfoBodySize)
	if err != nil {
		return fmt.Errorf("read PlaybackInfo response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Header.Set("Content-Length", strconv.FormatInt(int64(len(bodyBytes)), 10))
		resp.ContentLength = int64(len(bodyBytes))
		resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		return nil
	}

	respBody := bodyBytes
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		reader, err := gzip.NewReader(bytes.NewReader(bodyBytes))
		if err != nil {
			return fmt.Errorf("open gzip PlaybackInfo response: %w", err)
		}
		respBody, err = readLimited(reader, maxPlaybackInfoBodySize)
		closeErr := reader.Close()
		if err != nil {
			return fmt.Errorf("decompress PlaybackInfo response: %w", err)
		}
		if closeErr != nil {
			return fmt.Errorf("close gzip PlaybackInfo response: %w", closeErr)
		}
	default:
		return fmt.Errorf("unsupported PlaybackInfo content encoding")
	}

	newBody := s.modifyPlaybackInfo(respBody)
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("ETag")
	resp.Header.Del("Content-MD5")
	resp.Header.Del("Digest")
	resp.Header.Set("Cache-Control", "no-store")
	resp.Header.Set("Content-Length", strconv.FormatInt(int64(len(newBody)), 10))
	resp.ContentLength = int64(len(newBody))
	resp.Body = io.NopCloser(bytes.NewReader(newBody))
	return nil
}

func readAndCloseLimited(body io.ReadCloser, limit int64) ([]byte, error) {
	data, readErr := readLimited(body, limit)
	closeErr := body.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return data, nil
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return data, nil
}

func parseServerURL(addr string) (*url.URL, error) {
	normalized := strings.TrimSpace(addr)
	if !strings.Contains(normalized, "://") {
		normalized = "http://" + normalized
	}

	parsed, err := url.Parse(normalized)
	if err != nil {
		return nil, err
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("server address must use http or https")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return parsed, nil
}

func NormalizeServerAddress(addr string) (string, error) {
	parsed, err := parseServerURL(addr)
	if err != nil {
		return "", err
	}
	return parsed.String(), nil
}

func joinURLPath(baseURL, requestURL *url.URL) (string, string) {
	baseSlash := strings.HasSuffix(baseURL.Path, "/")
	requestSlash := strings.HasPrefix(requestURL.Path, "/")
	baseEscapedPath := baseURL.EscapedPath()
	requestEscapedPath := requestURL.EscapedPath()

	switch {
	case baseSlash && requestSlash:
		return baseURL.Path + requestURL.Path[1:], baseEscapedPath + requestEscapedPath[1:]
	case !baseSlash && !requestSlash:
		return baseURL.Path + "/" + requestURL.Path, baseEscapedPath + "/" + requestEscapedPath
	default:
		return baseURL.Path + requestURL.Path, baseEscapedPath + requestEscapedPath
	}
}

func joinURLQuery(baseQuery, requestQuery string) string {
	if baseQuery == "" {
		return requestQuery
	}
	if requestQuery == "" {
		return baseQuery
	}
	return baseQuery + "&" + requestQuery
}

func urlForLog(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return "<invalid-url>"
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	return parsed.Host + path
}

func errorWithoutURL(err error) string {
	originalErr := err
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		err = urlErr.Err
	}
	if err == nil {
		if originalErr == nil {
			return ""
		}
		return "unknown error"
	}
	return err.Error()
}

func (s *ProxyServer) modifyPlaybackInfo(body []byte) []byte {
	if !s.cfg.HttpStrm.DisableTranscode {
		return body
	}

	jsonStr := string(body)

	sources := gjson.Get(jsonStr, "MediaSources").Array()
	for i := range sources {
		prefix := fmt.Sprintf("MediaSources.%d", i)
		jsonStr, _ = sjson.Set(jsonStr, prefix+".SupportsDirectPlay", true)
		jsonStr, _ = sjson.Set(jsonStr, prefix+".SupportsDirectStream", true)
		jsonStr, _ = sjson.Set(jsonStr, prefix+".SupportsTranscoding", false)

		dUrl := gjson.Get(jsonStr, prefix+".DirectStreamUrl").String()
		if dUrl != "" {
			parsedURL, err := url.Parse(dUrl)
			if err == nil {
				query := parsedURL.Query()
				query.Set("Emby2Alist", "true")
				parsedURL.RawQuery = query.Encode()
				jsonStr, _ = sjson.Set(jsonStr, prefix+".DirectStreamUrl", parsedURL.String())
			}
		}
	}

	return []byte(jsonStr)
}
