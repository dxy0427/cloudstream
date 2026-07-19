package handlers

import (
	"cloudstream/internal/auth"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/openlist"
	"cloudstream/internal/pan123"
	"cloudstream/internal/webdav"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

func getStreamClient(account models.Account) interface{} {
	switch account.Type {
	case models.AccountTypeOpenList:
		return openlist.NewClient(account)
	case models.AccountTypeWebDAV:
		return webdav.NewClient(account)
	default:
		return pan123.NewClient(account)
	}
}

func UnifiedStreamHandler(c *gin.Context) {
	rawPath := c.Param("path")
	sign := c.Query("sign")

	var accountID uint
	var identifier interface{}
	var account models.Account

	if sign != "" {
		accID, realIdentity, err := auth.VerifyAccountStreamSign(sign)
		if err != nil {
			c.String(http.StatusForbidden, "Invalid signature")
			return
		}
		accountID = accID

		if err := database.DB.First(&account, accountID).Error; err != nil {
			c.String(http.StatusNotFound, "Account not found")
			return
		}
		if !account.EnableStreamSign {
			c.String(http.StatusForbidden, "Signed access is disabled for this account")
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
		if account.EnableStreamSign {
			c.String(http.StatusForbidden, "Signature required")
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
	case *openlist.Client:
		pathStr, ok := identifier.(string)
		if !ok {
			c.String(http.StatusBadRequest, "OpenList requires path identifier")
			return
		}
		downloadURL, err = cl.GetRawURLContext(c.Request.Context(), pathStr)
		redirectBase = cl.BaseURL
	case *webdav.Client:
		pathStr, ok := identifier.(string)
		if !ok {
			c.String(http.StatusBadRequest, "WebDAV requires path identifier")
			return
		}
		if models.NormalizePlaybackMode(account.PlaybackMode, account.Type) == models.PlaybackModeRedirect {
			redirectWebDAVDownload(c, cl, pathStr)
			return
		}
		proxyWebDAVDownload(c, cl, pathStr)
		return
	case *pan123.Client:
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
	if models.NormalizePlaybackMode(account.PlaybackMode, account.Type) == models.PlaybackModeRedirect {
		c.Header("Cache-Control", "no-store")
		c.Header("Referrer-Policy", "no-referrer")
		c.Redirect(http.StatusFound, downloadURL)
		return
	}
	proxyStreamURL(c, accountID, downloadURL)
}
