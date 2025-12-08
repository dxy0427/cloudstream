package handlers

import (
	"cloudstream/internal/auth"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/pan123"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"strings"
)

func UnifiedStreamHandler(c *gin.Context) {
	rawPath := c.Param("path")
	sign := c.Query("sign")

	var accountID uint
	var identifier interface{}

	if sign != "" {
		// 签名模式：验证并提取 RealIdentity (包含随机Salt，验证更严格)
		accID, realIdentity, err := auth.VerifyStreamSign(sign)
		if err != nil {
			c.String(http.StatusForbidden, "Invalid signature: "+err.Error())
			return
		}
		accountID = accID

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
		// 非签名模式
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

		var account models.Account
		if err := database.DB.First(&account, accountID).Error; err != nil {
			c.String(http.StatusNotFound, "Account not found")
			return
		}

		if account.Type == models.AccountTypeOpenList {
			identifier = "/" + strings.Join(parts[1:], "/")
		} else {
			// 123Pan: 第二部分必须是 FileID
			fileIdStr := parts[1]
			fileId, err := strconv.ParseInt(fileIdStr, 10, 64)
			if err != nil {
				c.String(http.StatusBadRequest, "Invalid File ID format for 123Pan")
				return
			}
			identifier = fileId
		}
	}

	var account models.Account
	if err := database.DB.First(&account, accountID).Error; err != nil {
		c.String(http.StatusNotFound, "Account not found")
		return
	}

	client := pan123.NewClient(account)
	downloadURL, err := client.GetDownloadURL(identifier)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("Failed to get link: %v", err))
		return
	}

	c.Redirect(http.StatusFound, downloadURL)
}