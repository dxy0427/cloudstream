package handlers

import (
	"cloudstream/internal/auth"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/openlist"
	"cloudstream/internal/pan123"
	"cloudstream/internal/webdav"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"syscall"

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

func openListAccountFromWebDAV(account models.Account) (models.Account, error) {
	rawURL := strings.TrimSpace(account.WebDAVURL)
	lowerURL := strings.ToLower(rawURL)
	if rawURL != "" && !strings.HasPrefix(lowerURL, "http://") && !strings.HasPrefix(lowerURL, "https://") {
		if strings.Contains(rawURL, "://") {
			return models.Account{}, errors.New("WebDAV 地址只支持 HTTP 或 HTTPS")
		}
		rawURL = "http://" + rawURL
	}
	webDAVURL, err := url.Parse(rawURL)
	if err != nil || webDAVURL.Host == "" || (webDAVURL.Scheme != "http" && webDAVURL.Scheme != "https") {
		return models.Account{}, errors.New("WebDAV 地址无效")
	}
	if webDAVURL.User != nil || webDAVURL.RawQuery != "" || webDAVURL.Fragment != "" {
		return models.Account{}, errors.New("WebDAV 地址不能包含用户信息、查询参数或片段")
	}
	trimmedPath := strings.TrimRight(webDAVURL.Path, "/")
	separator := strings.LastIndex(trimmedPath, "/")
	if separator < 0 || !strings.EqualFold(trimmedPath[separator+1:], "dav") {
		return models.Account{}, errors.New("WebDAV 地址必须以 /dav 结尾")
	}
	webDAVURL.Path = strings.TrimRight(trimmedPath[:separator], "/")
	webDAVURL.RawPath = ""

	openListAccount := account
	openListAccount.Type = models.AccountTypeOpenList
	openListAccount.OpenListURL = strings.TrimRight(webDAVURL.String(), "/")
	openListAccount.OpenListAuthMode = "password"
	openListAccount.OpenListUsername = account.WebDAVUsername
	openListAccount.OpenListPassword = account.WebDAVPassword
	openListAccount.OpenListToken = ""
	return openListAccount, nil
}

func validRedirectURL(value string) bool {
	redirectURL, err := url.Parse(value)
	return err == nil && redirectURL.User == nil && redirectURL.Host != "" && (redirectURL.Scheme == "http" || redirectURL.Scheme == "https")
}

func normalizeRedirectURL(value, trustedBase string) (string, bool) {
	redirectURL, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", false
	}
	if redirectURL.IsAbs() {
		if !validRedirectURL(redirectURL.String()) {
			return "", false
		}
		return redirectURL.String(), true
	}
	if trustedBase == "" || redirectURL.Host != "" || strings.HasPrefix(value, "//") {
		return "", false
	}
	baseURL, err := url.Parse(trustedBase)
	if err != nil || !validRedirectURL(baseURL.String()) {
		return "", false
	}
	resolveBase := *baseURL
	if !strings.HasPrefix(redirectURL.Path, "/") {
		resolveBase.Path = strings.TrimRight(resolveBase.Path, "/") + "/"
		resolveBase.RawPath = ""
	}
	resolved := resolveBase.ResolveReference(redirectURL)
	if resolved.Scheme != baseURL.Scheme || resolved.Host != baseURL.Host || !validRedirectURL(resolved.String()) {
		return "", false
	}
	return resolved.String(), true
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
	var redirectBase string
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
		redirectBase = cl.BaseURL
	case *cachedWebDAVClient:
		pathStr, ok := identifier.(string)
		if !ok {
			c.String(http.StatusBadRequest, "WebDAV requires path identifier")
			return
		}
		if account.WebDAVDirectLink {
			openListAccount, convertErr := openListAccountFromWebDAV(account)
			if convertErr != nil {
				err = convertErr
				break
			}
			downloadURL, err = openlist.NewClient(openListAccount).GetRawURLContext(c.Request.Context(), pathStr)
			redirectBase = openListAccount.OpenListURL
		} else {
			proxyWebDAVDownload(c, cl.Client, pathStr)
			return
		}
	case *cachedPan123Client:
		downloadURL, err = cl.GetDownloadURLContext(c.Request.Context(), identifier)
	}

	if err != nil {
		logUpstreamError("获取上游下载链接失败", accountID, err)
		c.String(http.StatusBadGateway, "Upstream service unavailable")
		return
	}
	downloadURL, ok := normalizeRedirectURL(downloadURL, redirectBase)
	if !ok {
		log.Error().Uint("accountID", accountID).Msg("上游返回了无效的下载地址")
		c.String(http.StatusBadGateway, "Upstream service unavailable")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
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
		if c.Request.Context().Err() != nil {
			return
		}
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
