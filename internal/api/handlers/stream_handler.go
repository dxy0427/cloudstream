package handlers

import (
	"cloudstream/internal/auth"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/openlist"
	"cloudstream/internal/pan123"
	"cloudstream/internal/webdav"
	"fmt"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// 客户端缓存，避免每次请求都新建
var (
	streamClientCache sync.Map // map[uint]interface{} — accountID → client
)

type cachedOpenListClient struct{ *openlist.Client }
type cachedWebDAVClient struct{ *webdav.Client }
type cachedPan123Client struct{ *pan123.Client }

func getStreamClient(account models.Account) interface{} {
	if cached, ok := streamClientCache.Load(account.ID); ok {
		return cached
	}

	var client interface{}
	switch account.Type {
	case models.AccountTypeOpenList:
		client = &cachedOpenListClient{openlist.NewClient(account)}
	case models.AccountTypeWebDAV:
		client = &cachedWebDAVClient{webdav.NewClient(account)}
	default:
		client = &cachedPan123Client{pan123.NewClient(account)}
	}

	streamClientCache.Store(account.ID, client)
	return client
}

// InvalidateStreamClient 缓存失效（账户更新/删除时调用）
func InvalidateStreamClient(accountID uint) {
	streamClientCache.Delete(accountID)
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
			c.String(http.StatusForbidden, "Invalid signature: "+err.Error())
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

	// 从缓存获取客户端
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
		downloadURL, err = cl.GetRawURL(pathStr)
	case *cachedWebDAVClient:
		pathStr, ok := identifier.(string)
		if !ok {
			c.String(http.StatusBadRequest, "WebDAV requires path identifier")
			return
		}
		proxyWebDAVDownload(c, cl.Client, pathStr)
		return
	case *cachedPan123Client:
		downloadURL, err = cl.GetDownloadURL(identifier)
	}

	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("Failed to get link: %v", err))
		return
	}
	c.Redirect(http.StatusFound, downloadURL)
}

func proxyWebDAVDownload(c *gin.Context, client *webdav.Client, pathStr string) {
	req, err := client.NewDownloadRequest(c.Request.Method, pathStr, nil)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("Failed to get link: %v", err))
		return
	}

	for _, name := range []string{"Range", "If-Range", "If-Modified-Since", "If-None-Match", "User-Agent"} {
		if value := c.GetHeader(name); value != "" {
			req.Header.Set(name, value)
		}
	}

	resp, err := client.HTTPClient.Do(req)
	if err != nil {
		c.String(http.StatusBadGateway, "WebDAV request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	for key, values := range resp.Header {
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}
	c.Status(resp.StatusCode)
	if c.Request.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(c.Writer, resp.Body)
}
