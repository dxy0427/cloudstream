package handlers

import (
	"cloudstream/internal/core"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/openlist"
	"cloudstream/internal/pan123"
	"cloudstream/internal/webdav"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

var accountTaskMutationMu sync.Mutex

type apiMutationError struct {
	status  int
	message string
}

func (e *apiMutationError) Error() string {
	return e.message
}

func newAPIMutationError(status int, message string) error {
	return &apiMutationError{status: status, message: message}
}

func isMutationUniqueConstraintError(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint failed") ||
		strings.Contains(message, "duplicate key") ||
		strings.Contains(message, "is not unique")
}

func respondMutationError(c *gin.Context, err error, fallbackMessage, conflictMessage string) {
	var apiErr *apiMutationError
	if errors.As(err, &apiErr) {
		c.JSON(apiErr.status, gin.H{"code": 1, "message": apiErr.message})
		return
	}
	if isMutationUniqueConstraintError(err) {
		log.Warn().Err(err).Msg(conflictMessage)
		c.JSON(http.StatusConflict, gin.H{"code": 1, "message": conflictMessage})
		return
	}
	log.Error().Err(err).Msg(fallbackMessage)
	c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": fallbackMessage})
}

func schedulerWarningResponse(message string, data any, err error) gin.H {
	response := gin.H{"code": 0, "message": message}
	if data != nil {
		response["data"] = data
	}
	if err != nil {
		log.Error().Err(err).Msg("刷新调度器失败")
		response["warning"] = "操作已保存，但调度器刷新失败"
	}
	return response
}

func ListAccountsHandler(c *gin.Context) {
	var accounts []models.Account
	if err := database.DB.Order("id asc").Find(&accounts).Error; err != nil {
		log.Error().Err(err).Msg("获取账户列表失败")
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "获取账户列表失败"})
		return
	}
	data := make([]gin.H, 0, len(accounts))
	for _, account := range accounts {
		data = append(data, sanitizeAccount(account))
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": data})
}

func sanitizeAccount(account models.Account) gin.H {
	return gin.H{
		"ID": account.ID, "CreatedAt": account.CreatedAt, "UpdatedAt": account.UpdatedAt,
		"Version": account.Version,
		"Name":    account.Name, "Type": account.Type, "ClientID": account.ClientID,
		"OpenListURL": account.OpenListURL, "OpenListAuthMode": normalizeOpenListAuthMode(&account), "OpenListUsername": account.OpenListUsername,
		"WebDAVURL": account.WebDAVURL, "WebDAVUsername": account.WebDAVUsername,
		"StrmBaseURL": account.StrmBaseURL, "PlaybackMode": models.NormalizePlaybackMode(account.PlaybackMode, account.Type),
		"EnableStreamSign": account.EnableStreamSign, "SignExpireHours": account.SignExpireHours, "CacheTTL": account.CacheTTL,
		"CustomCachePolicies": account.CustomCachePolicies,
	}
}

func accountDetail(account models.Account) gin.H {
	normalizeAccountType(&account)
	return gin.H{
		"ID": account.ID, "CreatedAt": account.CreatedAt, "UpdatedAt": account.UpdatedAt,
		"Version": account.Version,
		"Name":    account.Name, "Type": account.Type, "ClientID": account.ClientID, "ClientSecret": account.ClientSecret,
		"OpenListURL": account.OpenListURL, "OpenListAuthMode": normalizeOpenListAuthMode(&account),
		"OpenListToken": account.OpenListToken, "OpenListUsername": account.OpenListUsername, "OpenListPassword": account.OpenListPassword,
		"WebDAVURL": account.WebDAVURL, "WebDAVUsername": account.WebDAVUsername, "WebDAVPassword": account.WebDAVPassword,
		"StrmBaseURL": account.StrmBaseURL, "PlaybackMode": models.NormalizePlaybackMode(account.PlaybackMode, account.Type),
		"EnableStreamSign": account.EnableStreamSign, "SignExpireHours": account.SignExpireHours, "CacheTTL": account.CacheTTL,
		"CustomCachePolicies": account.CustomCachePolicies,
	}
}

func GetAccountHandler(c *gin.Context) {
	setSensitiveResponseHeaders(c)
	idValue, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || idValue == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "无效的账户ID"})
		return
	}

	var account models.Account
	if err := database.DB.First(&account, uint(idValue)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "账户未找到"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "读取账户详情失败"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": accountDetail(account)})
}

func normalizeAccountType(a *models.Account) {
	if a.Type == "" {
		a.Type = models.AccountType123Pan
	}
}

func normalizeOpenListAuthMode(a *models.Account) string {
	a.OpenListAuthMode = strings.TrimSpace(a.OpenListAuthMode)
	return a.OpenListAuthMode
}

func validateAccount(a *models.Account) (ok bool, msg string) {
	normalizeAccountType(a)
	if !models.IsValidPlaybackMode(a.PlaybackMode) {
		return false, "播放模式无效"
	}
	if a.SignExpireHours < 0 || a.SignExpireHours > 87600 {
		return false, "签名有效期必须在 0 到 87600 小时之间"
	}

	switch a.Type {
	case models.AccountType123Pan:
		if strings.TrimSpace(a.Name) == "" || strings.TrimSpace(a.ClientID) == "" || strings.TrimSpace(a.ClientSecret) == "" {
			return false, "123 云盘账户名称、ClientID、ClientSecret 不能为空"
		}
	case models.AccountTypeOpenList:
		if strings.TrimSpace(a.Name) == "" || strings.TrimSpace(a.OpenListURL) == "" {
			return false, "OpenList 账户名称和地址不能为空"
		}
		if normalizeOpenListAuthMode(a) != "token" && a.OpenListAuthMode != "password" {
			return false, "OpenList 认证方式无效"
		}
		if a.OpenListAuthMode == "token" && strings.TrimSpace(a.OpenListToken) == "" {
			return false, "OpenList Token 不能为空"
		}
		if a.OpenListAuthMode == "password" && (strings.TrimSpace(a.OpenListUsername) == "" || strings.TrimSpace(a.OpenListPassword) == "") {
			return false, "OpenList 用户名和密码不能为空"
		}
	case models.AccountTypeWebDAV:
		if strings.TrimSpace(a.Name) == "" || strings.TrimSpace(a.WebDAVURL) == "" {
			return false, "WebDAV 账户名称和地址不能为空"
		}
	default:
		return false, "不支持的云账户类型"
	}
	return true, ""
}

func normalizeAccountCredentials(a *models.Account) {
	normalizeAccountType(a)
	switch a.Type {
	case models.AccountType123Pan:
		a.OpenListURL = ""
		a.OpenListAuthMode = ""
		a.OpenListToken = ""
		a.OpenListUsername = ""
		a.OpenListPassword = ""
		a.WebDAVURL = ""
		a.WebDAVUsername = ""
		a.WebDAVPassword = ""
	case models.AccountTypeOpenList:
		a.ClientID = ""
		a.ClientSecret = ""
		a.WebDAVURL = ""
		a.WebDAVUsername = ""
		a.WebDAVPassword = ""
		normalizeInactiveOpenListCredentials(a)
	case models.AccountTypeWebDAV:
		a.ClientID = ""
		a.ClientSecret = ""
		a.OpenListURL = ""
		a.OpenListAuthMode = ""
		a.OpenListToken = ""
		a.OpenListUsername = ""
		a.OpenListPassword = ""
		if strings.TrimSpace(a.WebDAVUsername) == "" {
			a.WebDAVUsername = ""
		}
		if strings.TrimSpace(a.WebDAVPassword) == "" {
			a.WebDAVPassword = ""
		}
	}
}

type accountCreateRequest struct {
	models.Account
	PlaybackMode *string `json:"PlaybackMode"`
}

func applyPlaybackMode(account *models.Account, mode *string, fallback string) (bool, string) {
	resolved := models.NormalizePlaybackMode(fallback, account.Type)
	if mode != nil {
		if !models.IsValidPlaybackMode(*mode) {
			return false, "播放模式无效"
		}
		resolved = *mode
	}
	account.PlaybackMode = resolved
	return true, ""
}

func accountNameExists(tx *gorm.DB, name string, excludeID uint) (bool, error) {
	query := tx.Model(&models.Account{}).Where("name = ?", name)
	if excludeID != 0 {
		query = query.Where("id <> ?", excludeID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func CreateAccountHandler(c *gin.Context) {
	var req accountCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "账户参数错误"})
		return
	}
	account := req.Account
	normalizeAccountType(&account)
	if ok, msg := applyPlaybackMode(&account, req.PlaybackMode, models.DefaultPlaybackMode(account.Type)); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": msg})
		return
	}

	normalizeAccountCredentials(&account)
	if ok, msg := validateAccount(&account); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": msg})
		return
	}

	accountTaskMutationMu.Lock()
	defer accountTaskMutationMu.Unlock()

	requestedCacheTTL := account.CacheTTL
	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		exists, err := accountNameExists(tx, account.Name, 0)
		if err != nil {
			return err
		}
		if exists {
			return newAPIMutationError(http.StatusConflict, "账户名称已存在")
		}
		if err := tx.Create(&account).Error; err != nil {
			return err
		}
		if requestedCacheTTL == 0 {
			result := tx.Model(&models.Account{}).Where("id = ?", account.ID).Updates(map[string]any{"cache_ttl": 0})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return newAPIMutationError(http.StatusNotFound, "账户未找到")
			}
		}
		return tx.First(&account, account.ID).Error
	}); err != nil {
		respondMutationError(c, err, "创建账户失败", "账户名称已存在")
		return
	}
	account.CacheTTL = requestedCacheTTL
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": sanitizeAccount(account)})
}

type accountUpdateRequest struct {
	Version             *int    `json:"Version"`
	Name                *string `json:"Name"`
	Type                *string `json:"Type"`
	ClientID            *string `json:"ClientID"`
	ClientSecret        *string `json:"ClientSecret"`
	OpenListURL         *string `json:"OpenListURL"`
	OpenListAuthMode    *string `json:"OpenListAuthMode"`
	OpenListToken       *string `json:"OpenListToken"`
	OpenListUsername    *string `json:"OpenListUsername"`
	OpenListPassword    *string `json:"OpenListPassword"`
	WebDAVURL           *string `json:"WebDAVURL"`
	WebDAVUsername      *string `json:"WebDAVUsername"`
	WebDAVPassword      *string `json:"WebDAVPassword"`
	StrmBaseURL         *string `json:"StrmBaseURL"`
	PlaybackMode        *string `json:"PlaybackMode"`
	EnableStreamSign    *bool   `json:"EnableStreamSign"`
	SignExpireHours     *int    `json:"SignExpireHours"`
	CacheTTL            *int    `json:"CacheTTL"`
	CustomCachePolicies *string `json:"CustomCachePolicies"`
}

func UpdateAccountHandler(c *gin.Context) {
	idValue, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "无效的账户ID"})
		return
	}
	accountID := uint(idValue)

	var req accountUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "账户参数错误"})
		return
	}
	accountTaskMutationMu.Lock()
	defer accountTaskMutationMu.Unlock()
	var taskIDs []uint
	if err := database.DB.Model(&models.Task{}).Where("account_id = ?", accountID).Pluck("id", &taskIDs).Error; err != nil {
		respondMutationError(c, err, "查询关联任务失败", "任务名称已存在")
		return
	}
	if !core.BlockTasksIfIdle(taskIDs) {
		c.JSON(http.StatusConflict, gin.H{"code": 1, "message": "关联任务运行中，停止后才能修改账户"})
		return
	}
	defer core.UnblockTasks(taskIDs)

	var account models.Account
	err = database.DB.Transaction(func(tx *gorm.DB) error {
		var stored models.Account
		if err := tx.First(&stored, accountID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newAPIMutationError(http.StatusNotFound, "账户未找到")
			}
			return err
		}
		if req.Version == nil || *req.Version != stored.Version {
			return newAPIMutationError(http.StatusConflict, "账户配置已发生变化，请刷新后重试")
		}

		account = stored
		oldType := stored.Type
		normalizeAccountType(&stored)
		oldType = stored.Type
		if req.Type != nil {
			account.Type = *req.Type
		}
		normalizeAccountType(&account)
		typeChanged := account.Type != oldType
		if typeChanged {
			account.ClientID = ""
			account.ClientSecret = ""
			account.OpenListURL = ""
			account.OpenListAuthMode = ""
			account.OpenListToken = ""
			account.OpenListUsername = ""
			account.OpenListPassword = ""
			account.WebDAVURL = ""
			account.WebDAVUsername = ""
			account.WebDAVPassword = ""

			var taskCount int64
			if err := tx.Model(&models.Task{}).Where("account_id = ?", accountID).Count(&taskCount).Error; err != nil {
				return err
			}
			if taskCount > 0 {
				return newAPIMutationError(http.StatusConflict, "账户存在关联任务，不能更改账户类型")
			}
		}

		if req.Name != nil {
			account.Name = *req.Name
		}
		if req.ClientID != nil {
			account.ClientID = *req.ClientID
		}
		if req.ClientSecret != nil {
			account.ClientSecret = *req.ClientSecret
		}
		if req.OpenListURL != nil {
			account.OpenListURL = *req.OpenListURL
		}
		if req.OpenListAuthMode != nil {
			account.OpenListAuthMode = *req.OpenListAuthMode
		}
		if req.OpenListToken != nil {
			account.OpenListToken = *req.OpenListToken
		}
		if req.OpenListUsername != nil {
			account.OpenListUsername = *req.OpenListUsername
		}
		if req.OpenListPassword != nil {
			account.OpenListPassword = *req.OpenListPassword
		}
		if req.WebDAVURL != nil {
			account.WebDAVURL = *req.WebDAVURL
		}
		if req.WebDAVUsername != nil {
			account.WebDAVUsername = *req.WebDAVUsername
		}
		if req.WebDAVPassword != nil {
			account.WebDAVPassword = *req.WebDAVPassword
		}
		if ok, msg := applyPlaybackMode(&account, req.PlaybackMode, stored.PlaybackMode); !ok {
			return newAPIMutationError(http.StatusBadRequest, msg)
		}
		if req.StrmBaseURL != nil {
			account.StrmBaseURL = *req.StrmBaseURL
		}
		if req.EnableStreamSign != nil {
			account.EnableStreamSign = *req.EnableStreamSign
		}
		if req.SignExpireHours != nil {
			account.SignExpireHours = *req.SignExpireHours
		}
		if req.CacheTTL != nil {
			account.CacheTTL = *req.CacheTTL
		}
		if req.CustomCachePolicies != nil {
			account.CustomCachePolicies = *req.CustomCachePolicies
		}

		normalizeAccountCredentials(&account)

		if ok, msg := validateAccount(&account); !ok {
			return newAPIMutationError(http.StatusBadRequest, msg)
		}
		exists, err := accountNameExists(tx, account.Name, accountID)
		if err != nil {
			return err
		}
		if exists {
			return newAPIMutationError(http.StatusConflict, "账户名称已存在")
		}

		account.Version = stored.Version + 1
		result := tx.Model(&models.Account{}).Where("id = ? AND version = ?", accountID, stored.Version).Updates(map[string]any{
			"version":               account.Version,
			"name":                  account.Name,
			"type":                  account.Type,
			"client_id":             account.ClientID,
			"client_secret":         account.ClientSecret,
			"open_list_url":         account.OpenListURL,
			"open_list_auth_mode":   account.OpenListAuthMode,
			"open_list_token":       account.OpenListToken,
			"open_list_username":    account.OpenListUsername,
			"open_list_password":    account.OpenListPassword,
			"web_dav_url":           account.WebDAVURL,
			"web_dav_username":      account.WebDAVUsername,
			"web_dav_password":      account.WebDAVPassword,
			"strm_base_url":         account.StrmBaseURL,
			"playback_mode":         account.PlaybackMode,
			"enable_stream_sign":    account.EnableStreamSign,
			"sign_expire_hours":     account.SignExpireHours,
			"cache_ttl":             account.CacheTTL,
			"custom_cache_policies": account.CustomCachePolicies,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return newAPIMutationError(http.StatusConflict, "账户配置已发生变化，请刷新后重试")
		}
		return tx.First(&account, accountID).Error
	})
	if err != nil {
		respondMutationError(c, err, "更新账户失败", "账户名称已存在")
		return
	}
	openlist.InvalidateAccountCache(account.ID)
	pan123.InvalidateAccountCache(account.ID)
	webdav.InvalidateAccountCache(account.ID)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": sanitizeAccount(account)})
}

func DeleteAccountHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "无效的账户ID"})
		return
	}
	accountID := uint(id)

	accountTaskMutationMu.Lock()
	defer accountTaskMutationMu.Unlock()

	var account models.Account
	if err := database.DB.First(&account, accountID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "账户未找到"})
			return
		}
		respondMutationError(c, err, "查询账户失败", "账户名称已存在")
		return
	}

	var tasksUsingAccount []models.Task
	if err := database.DB.Where("account_id = ?", accountID).Find(&tasksUsingAccount).Error; err != nil {
		respondMutationError(c, err, "查询关联任务失败", "任务名称已存在")
		return
	}

	taskIDs := make([]uint, 0, len(tasksUsingAccount))
	for _, task := range tasksUsingAccount {
		taskIDs = append(taskIDs, task.ID)
	}
	if !core.StopTasksAndWait(taskIDs, 30*time.Second) {
		core.UnblockTasks(taskIDs)
		c.JSON(http.StatusConflict, gin.H{"code": 1, "message": "关联任务停止超时，未执行删除"})
		return
	}
	defer core.UnblockTasks(taskIDs)

	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("task_id IN (?)", tx.Model(&models.Task{}).Select("id").Where("account_id = ?", accountID)).Delete(&models.TaskFile{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("account_id = ?", accountID).Delete(&models.Task{}).Error; err != nil {
			return err
		}
		result := tx.Unscoped().Where("id = ?", accountID).Delete(&models.Account{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return newAPIMutationError(http.StatusNotFound, "账户未找到")
		}
		return nil
	}); err != nil {
		respondMutationError(c, err, "删除账户失败", "账户名称已存在")
		return
	}
	openlist.InvalidateAccountCache(accountID)
	pan123.InvalidateAccountCache(accountID)
	webdav.InvalidateAccountCache(accountID)
	refreshErr := core.RefreshScheduler()
	c.JSON(http.StatusOK, schedulerWarningResponse("账户及关联任务已删除", nil, refreshErr))
}
