package handlers

import (
	"cloudstream/internal/auth"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/openlist"
	"cloudstream/internal/pan123"
	"cloudstream/internal/webdav"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

type cachedOpenListClient struct{ *openlist.Client }
type cachedWebDAVClient struct{ *webdav.Client }
type cachedPan123Client struct{ *pan123.Client }

var upstreamURLPattern = regexp.MustCompile(`(?i)https?://[^\s"']+`)

func getStreamClient(account models.Account) interface{} {
	switch account.Type {
	case models.AccountTypeOpenList:
		return &cachedOpenListClient{openlist.NewClient(account)}
	case models.AccountTypeWebDAV:
		return &cachedWebDAVClient{webdav.NewClient(account)}
	default:
		return &cachedPan123Client{pan123.NewClient(account)}
	}
}

// InvalidateStreamClient is kept for callers; stream clients are not cached.
func InvalidateStreamClient(accountID uint) {}

func logUpstreamError(message string, accountID uint, err error) {
	redacted := upstreamURLPattern.ReplaceAllString(err.Error(), "[redacted-url]")
	log.Error().Uint("accountID", accountID).Str("error", redacted).Msg(message)
}

func UnifiedStreamHandler(c *gin.Context) {
	rawPath := c.Param("path")
	sign := c.Query("sign")

	var accountID uint
	var identifier interface{}
	var account models.Account

	if sign != "" {
		taskID, accID, realIdentity, err := auth.VerifyStreamSign(sign)
		if err != nil {
			c.String(http.StatusForbidden, "Invalid signature")
			return
		}
		accountID = accID

		var task models.Task
		if err := database.DB.First(&task, taskID).Error; err != nil {
			c.String(http.StatusNotFound, "Task not found")
			return
		}
		if task.AccountID != accountID {
			c.String(http.StatusForbidden, "Task/account mismatch")
			return
		}
		if !task.Enabled || !task.EncodePath {
			c.String(http.StatusForbidden, "Signed access is disabled for this task")
			return
		}

		if err := database.DB.First(&account, accountID).Error; err != nil {
			c.String(http.StatusNotFound, "Account not found")
			return
		}

		if strings.HasPrefix(realIdentity, "/") {
			identifier = realIdentity
		} else {
			if id, err := strconv.ParseInt(realIdentity, 10, 64); err == nil {
				identifier = id
			} else {
				identifier = realIdentity
			}
		}
	} else {
		trimmedPath := strings.TrimPrefix(rawPath, "/")
		parts := strings.Split(trimmedPath, "/")

		if len(parts) < 2 {
			c.String(http.StatusBadRequest, "Invalid URL format")
			return
		}

		idUint, err := strconv.ParseUint(parts[0], 10, 32)
		if err != nil {
			c.String(http.StatusBadRequest, "Invalid Account ID")
			return
		}
		accountID = uint(idUint)

		if err := database.DB.First(&account, accountID).Error; err != nil {
			c.String(http.StatusNotFound, "Account not found")
			return
		}

		if account.Type == models.AccountTypeOpenList || account.Type == models.AccountTypeWebDAV {
			pathPart := "/" + strings.Join(parts[1:], "/")
			identifier = strings.ReplaceAll(pathPart, "//", "/")
		} else {
			fileIdStr := parts[1]
			fileId, err := strconv.ParseInt(fileIdStr, 10, 64)
			if err != nil {
				c.String(http.StatusBadRequest, "Invalid File ID format for 123Pan")
				return
			}
			identifier = fileId
		}
	}

	var downloadURL string
	var err error

	client := getStreamClient(account)
	switch cl := client.(type) {
	case *cachedOpenListClient:
		pathStr, ok := identifier.(string)
		if !ok {
			c.String(http.StatusBadRequest, "OpenList requires path identifier")
			return
		}
		downloadURL, err = cl.GetRawURLContext(c.Request.Context(), pathStr)
	case *cachedWebDAVClient:
		pathStr, ok := identifier.(string)
		if !ok {
			c.String(http.StatusBadRequest, "WebDAV requires path identifier")
			return
		}
		proxyWebDAVDownload(c, cl.Client, pathStr)
		return
	case *cachedPan123Client:
		downloadURL, err = cl.GetDownloadURLContext(c.Request.Context(), identifier)
	}

	if err != nil {
		logUpstreamError("获取上游下载链接失败", accountID, err)
		c.String(http.StatusBadGateway, "Upstream service unavailable")
		return
	}
	c.Redirect(http.StatusFound, downloadURL)
}

func proxyWebDAVDownload(c *gin.Context, client *webdav.Client, pathStr string) {
	req, err := client.NewDownloadRequest(c.Request.Context(), c.Request.Method, pathStr, nil)
	if err != nil {
		logUpstreamError("创建 WebDAV 上游请求失败", client.AccountID, err)
		c.String(http.StatusBadGateway, "Upstream service unavailable")
		return
	}

	for _, name := range []string{"Range", "If-Range", "If-Modified-Since", "If-None-Match", "User-Agent"} {
		if value := c.GetHeader(name); value != "" {
			req.Header.Set(name, value)
		}
	}

	resp, err := client.HTTPClient.Do(req)
	if err != nil {
		logUpstreamError("WebDAV 上游请求失败", client.AccountID, err)
		c.String(http.StatusBadGateway, "Upstream service unavailable")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		log.Error().Uint("accountID", client.AccountID).Int("upstreamStatus", resp.StatusCode).Msg("WebDAV 上游返回错误状态")
		c.String(http.StatusBadGateway, "Upstream service unavailable")
		return
	}

	for _, key := range []string{
		"Accept-Ranges",
		"Cache-Control",
		"Content-Disposition",
		"Content-Length",
		"Content-Range",
		"Content-Type",
		"ETag",
		"Expires",
		"Last-Modified",
	} {
		values := resp.Header.Values(key)
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}
	c.Status(resp.StatusCode)
	if c.Request.Method == http.MethodHead {
		return
	}
	if _, err := io.Copy(c.Writer, resp.Body); err != nil {
		logUpstreamError("转发 WebDAV 响应失败", client.AccountID, err)
	}
}
