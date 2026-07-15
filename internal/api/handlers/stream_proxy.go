package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

const maxStreamRedirects = 10

var (
	streamRequestHeaderNames = []string{
		"Range",
		"If-Range",
		"If-Match",
		"If-None-Match",
		"If-Modified-Since",
		"If-Unmodified-Since",
		"User-Agent",
		"Accept",
	}
	streamResponseHeaderNames = []string{
		"Accept-Ranges",
		"Cache-Control",
		"Content-Disposition",
		"Content-Encoding",
		"Content-Length",
		"Content-Range",
		"Content-Type",
		"ETag",
		"Expires",
		"Last-Modified",
		"Vary",
	}
	streamProxyTransport = &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:    true,
	}
	streamProxyHTTPClient = &http.Client{
		Transport: streamProxyTransport,
		Timeout:   0,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
)

func streamRequestHeaders(c *gin.Context) http.Header {
	return sanitizedStreamRequestHeaders(c.Request.Header)
}

func sanitizedStreamRequestHeaders(source http.Header) http.Header {
	headers := make(http.Header)
	copySelectedHeaders(headers, source, streamRequestHeaderNames)
	headers.Set("Accept-Encoding", "identity")
	return headers
}

func proxyStreamURL(c *gin.Context, accountID uint, streamURL string) {
	if !validStreamURL(streamURL) {
		log.Error().Uint("accountID", accountID).Msg("upstream returned an invalid stream URL")
		writeStreamBadGateway(c)
		return
	}

	request, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, streamURL, nil)
	if err != nil {
		logUpstreamError("failed to create upstream stream request", accountID, err)
		writeStreamBadGateway(c)
		return
	}
	request.Header = streamRequestHeaders(c)
	response, err := streamProxyHTTPClient.Do(request)
	if err != nil {
		if c.Request.Context().Err() != nil {
			return
		}
		logUpstreamError("upstream stream request failed", accountID, err)
		writeStreamBadGateway(c)
		return
	}
	proxyStreamResponse(c, accountID, response)
}

func proxyStreamResponse(c *gin.Context, accountID uint, response *http.Response) {
	response, err := followStreamRedirects(c.Request.Context(), streamProxyHTTPClient, response, c.Request.Method, streamRequestHeaders(c))
	if err != nil {
		if c.Request.Context().Err() != nil {
			return
		}
		logUpstreamError("failed to follow upstream stream redirect", accountID, err)
		writeStreamBadGateway(c)
		return
	}
	defer response.Body.Close()
	writeStreamResponse(c, accountID, response)
}

func followStreamRedirects(ctx context.Context, client *http.Client, response *http.Response, method string, headers http.Header) (*http.Response, error) {
	if response == nil {
		return nil, errors.New("upstream stream response is nil")
	}
	if client == nil {
		client = streamProxyHTTPClient
	}
	headers = sanitizedStreamRequestHeaders(headers)

	current := response
	for redirectCount := 0; isStreamRedirectStatus(current.StatusCode); redirectCount++ {
		if redirectCount >= maxStreamRedirects {
			current.Body.Close()
			return nil, fmt.Errorf("upstream stream exceeded %d redirects", maxStreamRedirects)
		}
		location, err := current.Location()
		current.Body.Close()
		if err != nil || !validStreamURL(location.String()) {
			return nil, errors.New("upstream returned an invalid stream redirect URL")
		}

		request, err := http.NewRequestWithContext(ctx, method, location.String(), nil)
		if err != nil {
			return nil, err
		}
		request.Header = headers.Clone()
		current, err = client.Do(request)
		if err != nil {
			return nil, err
		}
	}
	return current, nil
}

func writeStreamResponse(c *gin.Context, accountID uint, response *http.Response) {
	status := response.StatusCode
	if !isStreamSuccessStatus(status) {
		log.Error().Uint("accountID", accountID).Int("upstreamStatus", status).Msg("upstream stream returned an unsupported status")
		writeStreamBadGateway(c)
		return
	}

	headerNames := streamResponseHeaderNames
	switch status {
	case http.StatusNotModified:
		headerNames = []string{"Cache-Control", "ETag", "Expires", "Last-Modified", "Vary"}
	case http.StatusPreconditionFailed:
		headerNames = []string{"Cache-Control", "ETag", "Expires", "Last-Modified", "Vary"}
	case http.StatusRequestedRangeNotSatisfiable:
		headerNames = []string{"Accept-Ranges", "Content-Range"}
	case http.StatusNoContent, http.StatusResetContent:
		headerNames = []string{"Cache-Control", "ETag", "Expires", "Last-Modified", "Vary"}
	}
	copySelectedHeaders(c.Writer.Header(), response.Header, headerNames)
	c.Status(status)
	c.Writer.WriteHeaderNow()
	if c.Request.Method == http.MethodHead || status == http.StatusNotModified || status == http.StatusPreconditionFailed || status == http.StatusRequestedRangeNotSatisfiable || status == http.StatusNoContent || status == http.StatusResetContent {
		return
	}

	upstream := &errorTrackingReader{reader: response.Body}
	if _, err := io.Copy(c.Writer, upstream); err != nil {
		if upstream.err != nil {
			if c.Request.Context().Err() == nil && !errors.Is(upstream.err, context.Canceled) {
				logUpstreamError("failed to read upstream stream response", accountID, upstream.err)
			}
			return
		}
		if isExpectedDownstreamDisconnect(c.Request.Context(), err) {
			log.Debug().Uint("accountID", accountID).Msg("stream client disconnected")
			return
		}
		logUpstreamError("failed to forward upstream stream response", accountID, err)
	}
}

func writeStreamBadGateway(c *gin.Context) {
	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusBadGateway)
		c.Writer.WriteHeaderNow()
		return
	}
	c.String(http.StatusBadGateway, "Upstream service unavailable")
}

func validStreamURL(value string) bool {
	streamURL, err := url.Parse(value)
	return err == nil && streamURL.User == nil && streamURL.Host != "" && (streamURL.Scheme == "http" || streamURL.Scheme == "https")
}

func isStreamRedirectStatus(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

func isStreamSuccessStatus(status int) bool {
	return status >= http.StatusOK && status < http.StatusMultipleChoices || status == http.StatusNotModified || status == http.StatusPreconditionFailed || status == http.StatusRequestedRangeNotSatisfiable
}

func noRedirectHTTPClient(base *http.Client) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	clone := *base
	client := &clone
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	client.Timeout = 0
	return client
}

func copySelectedHeaders(destination, source http.Header, names []string) {
	for _, key := range names {
		for _, value := range source.Values(key) {
			destination.Add(key, value)
		}
	}
}

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
