package handlers

import (
	"cloudstream/internal/webdav"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

func redirectWebDAVDownload(c *gin.Context, client *webdav.Client, pathStr string) {
	if streamURL, err := client.GetDownloadURL(pathStr); err != nil || !validStreamURL(streamURL) {
		log.Error().Uint("accountID", client.AccountID).Msg("WebDAV download URL is invalid")
		writeStreamBadGateway(c)
		return
	}
	requestClient := noRedirectHTTPClient(client.HTTPClient)
	headers := streamRequestHeaders(c)
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
		headers.Del("If-Match")
		headers.Del("If-Modified-Since")
		headers.Del("If-None-Match")
		headers.Del("If-Range")
		headers.Del("If-Unmodified-Since")
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

	if !isStreamRedirectStatus(resp.StatusCode) {
		if resp.StatusCode == http.StatusNotModified {
			copySelectedHeaders(c.Writer.Header(), resp.Header, []string{"Cache-Control", "ETag", "Expires", "Last-Modified"})
			c.Status(http.StatusNotModified)
			c.Writer.WriteHeaderNow()
			return
		}
		if resp.StatusCode == http.StatusPreconditionFailed {
			copySelectedHeaders(c.Writer.Header(), resp.Header, []string{"Cache-Control", "ETag", "Expires", "Last-Modified", "Vary"})
			c.Status(http.StatusPreconditionFailed)
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
	if err != nil || !validStreamURL(location.String()) {
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

func proxyWebDAVDownload(c *gin.Context, client *webdav.Client, pathStr string) {
	if streamURL, err := client.GetDownloadURL(pathStr); err != nil || !validStreamURL(streamURL) {
		log.Error().Uint("accountID", client.AccountID).Msg("WebDAV download URL is invalid")
		writeStreamBadGateway(c)
		return
	}
	requestClient := noRedirectHTTPClient(client.HTTPClient)
	resp, err := client.DoDownloadRequest(c.Request.Context(), requestClient, c.Request.Method, pathStr, streamRequestHeaders(c))
	if err != nil {
		if c.Request.Context().Err() != nil {
			return
		}
		logUpstreamError("WebDAV 上游请求失败", client.AccountID, err)
		c.String(http.StatusBadGateway, "Upstream service unavailable")
		return
	}
	proxyStreamResponse(c, client.AccountID, resp)
}
