package handlers

import (
	"cloudstream/internal/webdav"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"syscall"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

func isExpectedDownstreamDisconnect(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	return errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNRESET)
}

func redirectWebDAVDownload(c *gin.Context, client *webdav.Client, pathStr string) {
	requestClient := http.DefaultClient
	if client.HTTPClient != nil {
		clone := *client.HTTPClient
		requestClient = &clone
	}
	requestClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	headers := webDAVRequestHeaders(c)
	resp, err := client.DoDownloadRequest(c.Request.Context(), requestClient, c.Request.Method, pathStr, headers)
	if err != nil {
		if c.Request.Context().Err() != nil {
			return
		}
		logUpstreamError("WebDAV 直链请求失败", client.AccountID, err)
		c.String(http.StatusBadGateway, "Upstream service unavailable")
		return
	}
	if c.Request.Method == http.MethodHead && shouldProbeWebDAVRedirect(resp.StatusCode) {
		resp.Body.Close()
		headers.Del("If-Modified-Since")
		headers.Del("If-None-Match")
		headers.Del("If-Range")
		headers.Set("Range", "bytes=0-0")
		resp, err = client.DoDownloadRequest(c.Request.Context(), requestClient, http.MethodGet, pathStr, headers)
		if err != nil {
			if c.Request.Context().Err() != nil {
				return
			}
			logUpstreamError("探测 WebDAV 重定向失败", client.AccountID, err)
			c.String(http.StatusBadGateway, "Upstream service unavailable")
			return
		}
	}
	defer resp.Body.Close()

	if !isWebDAVRedirectStatus(resp.StatusCode) {
		if resp.StatusCode == http.StatusNotModified {
			copySelectedHeaders(c.Writer.Header(), resp.Header, []string{"Cache-Control", "ETag", "Expires", "Last-Modified"})
			c.Status(http.StatusNotModified)
			c.Writer.WriteHeaderNow()
			return
		}
		if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
			copySelectedHeaders(c.Writer.Header(), resp.Header, []string{"Accept-Ranges", "Content-Range"})
			c.Status(http.StatusRequestedRangeNotSatisfiable)
			c.Writer.WriteHeaderNow()
			return
		}
		if resp.StatusCode >= http.StatusBadRequest {
			log.Error().Uint("accountID", client.AccountID).Int("upstreamStatus", resp.StatusCode).Msg("WebDAV 直链上游返回错误状态")
		} else {
			log.Warn().Uint("accountID", client.AccountID).Int("upstreamStatus", resp.StatusCode).Msg("WebDAV 上游未返回重定向，请改用代理模式")
		}
		c.String(http.StatusBadGateway, "WebDAV 上游未返回重定向，请切换为本机代理模式")
		return
	}

	location, err := resp.Location()
	if err != nil || !validRedirectURL(location.String()) {
		log.Error().Uint("accountID", client.AccountID).Msg("WebDAV 上游返回了无效的重定向地址")
		c.String(http.StatusBadGateway, "Upstream service unavailable")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Redirect(resp.StatusCode, location.String())
}

func shouldProbeWebDAVRedirect(status int) bool {
	return status == http.StatusOK || status == http.StatusNoContent || status == http.StatusMethodNotAllowed || status == http.StatusNotImplemented
}

func isWebDAVRedirectStatus(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

func webDAVRequestHeaders(c *gin.Context) http.Header {
	headers := make(http.Header)
	for _, name := range []string{"Range", "If-Range", "If-Modified-Since", "If-None-Match", "User-Agent"} {
		if value := c.GetHeader(name); value != "" {
			headers.Set(name, value)
		}
	}
	return headers
}

func proxyWebDAVDownload(c *gin.Context, client *webdav.Client, pathStr string) {
	requestClient := webDAVNoRedirectHTTPClient(client.HTTPClient)
	resp, err := client.DoDownloadRequest(c.Request.Context(), requestClient, c.Request.Method, pathStr, webDAVRequestHeaders(c))
	if err != nil {
		if c.Request.Context().Err() != nil {
			return
		}
		logUpstreamError("WebDAV 上游请求失败", client.AccountID, err)
		c.String(http.StatusBadGateway, "Upstream service unavailable")
		return
	}
	resp, err = followWebDAVRedirects(c.Request.Context(), requestClient, resp, c.Request.Method, webDAVRequestHeaders(c))
	if err != nil {
		if c.Request.Context().Err() != nil {
			return
		}
		logUpstreamError("跟随 WebDAV 上游重定向失败", client.AccountID, err)
		c.String(http.StatusBadGateway, "Upstream service unavailable")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		copySelectedHeaders(c.Writer.Header(), resp.Header, []string{"Accept-Ranges", "Content-Range"})
		c.Status(http.StatusRequestedRangeNotSatisfiable)
		c.Writer.WriteHeaderNow()
		return
	}
	if resp.StatusCode >= http.StatusBadRequest {
		log.Error().Uint("accountID", client.AccountID).Int("upstreamStatus", resp.StatusCode).Msg("WebDAV 上游返回错误状态")
		c.String(http.StatusBadGateway, "Upstream service unavailable")
		return
	}

	copySelectedHeaders(c.Writer.Header(), resp.Header, []string{
		"Accept-Ranges",
		"Cache-Control",
		"Content-Disposition",
		"Content-Length",
		"Content-Range",
		"Content-Type",
		"ETag",
		"Expires",
		"Last-Modified",
	})
	c.Status(resp.StatusCode)
	if c.Request.Method == http.MethodHead {
		return
	}
	upstream := &errorTrackingReader{reader: resp.Body}
	if _, err := io.Copy(c.Writer, upstream); err != nil {
		if upstream.err != nil {
			if c.Request.Context().Err() == nil && !errors.Is(upstream.err, context.Canceled) {
				logUpstreamError("读取 WebDAV 上游响应失败", client.AccountID, upstream.err)
			}
			return
		}
		if isExpectedDownstreamDisconnect(c.Request.Context(), err) {
			log.Debug().Uint("accountID", client.AccountID).Msg("WebDAV 客户端已结束分段请求")
			return
		}
		logUpstreamError("转发 WebDAV 响应失败", client.AccountID, err)
	}
}

func webDAVNoRedirectHTTPClient(base *http.Client) *http.Client {
	client := http.DefaultClient
	if base != nil {
		clone := *base
		client = &clone
	}
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client
}

func followWebDAVRedirects(ctx context.Context, baseClient *http.Client, response *http.Response, method string, headers http.Header) (*http.Response, error) {
	current := response
	for redirectCount := 0; isWebDAVRedirectStatus(current.StatusCode); redirectCount++ {
		if redirectCount >= 10 {
			current.Body.Close()
			return nil, errors.New("WebDAV 重定向次数过多")
		}
		location, err := current.Location()
		current.Body.Close()
		if err != nil || !validRedirectURL(location.String()) {
			return nil, errors.New("WebDAV 上游返回了无效的重定向地址")
		}
		requestMethod := method
		if method != http.MethodHead && (current.StatusCode == http.StatusSeeOther || ((current.StatusCode == http.StatusMovedPermanently || current.StatusCode == http.StatusFound) && method != http.MethodGet)) {
			requestMethod = http.MethodGet
		}
		request, err := http.NewRequestWithContext(ctx, requestMethod, location.String(), nil)
		if err != nil {
			return nil, err
		}
		request.Header = headers.Clone()
		request.Header.Del("Authorization")
		request.Header.Del("Proxy-Authorization")
		request.Header.Del("Cookie")
		request.Header.Del("Cookie2")
		current, err = baseClient.Do(request)
		if err != nil {
			return nil, err
		}
		method = requestMethod
	}
	return current, nil
}

func copySelectedHeaders(destination, source http.Header, names []string) {
	for _, key := range names {
		values := source.Values(key)
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}

type errorTrackingReader struct {
	reader io.Reader
	err    error
}

func (reader *errorTrackingReader) Read(buffer []byte) (int, error) {
	read, err := reader.reader.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		reader.err = err
	}
	return read, err
}
