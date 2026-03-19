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
		accID, realIdentity, err := auth.VerifyStreamSign(sign)
		if err != nil {
			c.String(http.StatusForbidden, "Invalid signature: "+err.Error())
			return
		}
		accountID = accID

		var account models.Account
		if err := database.DB.First(&account, accountID).Error; err != nil {
			c.String(http.StatusNotFound, "Account not found")
			return
		}
		if !isTaskEncodePathEnabled(accountID) {
			c.String(http.StatusForbidden, "Signed access is only available when encrypted path is enabled")
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

		var account models.Account
		if err := database.DB.First(&account, accountID).Error; err != nil {
			c.String(http.StatusNotFound, "Account not found")
			return
		}
		if isTaskEncodePathEnabled(accountID) {
			c.String(http.StatusForbidden, "This stream requires a valid signature")
			return
		}

		if account.Type == models.AccountTypeOpenList {
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

func isTaskEncodePathEnabled(accountID uint) bool {
	var count int64
	database.DB.Model(&models.Task{}).
		Where("account_id = ? AND enabled = ? AND encode_path = ?", accountID, true, true).
		Count(&count)
	return count > 0
}
