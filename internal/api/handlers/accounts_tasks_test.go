package handlers

import (
	"bytes"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := database.DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "handlers.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Account{}, &models.Task{}, &models.TaskFile{}); err != nil {
		t.Fatal(err)
	}
	database.DB = db
	t.Cleanup(func() {
		database.DB = previousDB
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func performHandlerRequest(t *testing.T, method, path string, payload any, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			t.Fatal(err)
		}
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, path, &body)
	ctx.Request.Header.Set("Content-Type", "application/json")
	if requestPath := ctx.Request.URL.Path; requestPath != "" {
		if index := strings.LastIndex(requestPath, "/"); index >= 0 {
			ctx.Params = gin.Params{{Key: "id", Value: requestPath[index+1:]}}
		}
	}
	handler(ctx)
	return recorder
}

func TestUpdateAccountRejectsMalformedID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := performHandlerRequest(t, http.MethodPut, "/accounts/1abc", map[string]any{"Name": "renamed"}, UpdateAccountHandler)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func assertSensitiveResponseHeaders(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	expected := map[string]string{
		"Cache-Control":          "no-store, max-age=0",
		"Pragma":                 "no-cache",
		"Expires":                "0",
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
	}
	for name, value := range expected {
		if got := recorder.Header().Get(name); got != value {
			t.Fatalf("%s = %q, want %q", name, got, value)
		}
	}
}

func TestAccountListRedactsAndDetailReturnsPlaintext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{
		Name:                "account-detail",
		Type:                models.AccountTypeWebDAV,
		ClientID:            "client-id",
		ClientSecret:        "saved-client-secret",
		OpenListURL:         "https://list.example",
		OpenListAuthMode:    "token",
		OpenListToken:       "saved-openlist-token",
		OpenListUsername:    "openlist-user",
		OpenListPassword:    "saved-openlist-password",
		WebDAVURL:           "https://dav.example",
		WebDAVUsername:      "dav-user",
		WebDAVPassword:      "saved-webdav-password",
		PlaybackMode:        models.PlaybackModeProxy,
		CustomCachePolicies: "/media/*:10",
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}

	listed := performHandlerRequest(t, http.MethodGet, "/accounts", nil, ListAccountsHandler)
	if listed.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listed.Code, listed.Body.String())
	}
	for _, value := range []string{account.ClientSecret, account.OpenListToken, account.OpenListPassword, account.WebDAVPassword} {
		if strings.Contains(listed.Body.String(), value) {
			t.Fatalf("account list leaked %q: %s", value, listed.Body.String())
		}
	}
	for _, field := range []string{"ClientSecret", "OpenListToken", "OpenListPassword", "WebDAVPassword", "HasClientSecret"} {
		if strings.Contains(listed.Body.String(), `"`+field+`"`) {
			t.Fatalf("account list exposed sensitive metadata %q: %s", field, listed.Body.String())
		}
	}
	var listResponse struct {
		Data []models.Account `json:"data"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &listResponse); err != nil {
		t.Fatal(err)
	}
	if len(listResponse.Data) != 1 || listResponse.Data[0].Version != account.Version {
		t.Fatalf("account list version = %+v, want %d", listResponse.Data, account.Version)
	}

	detail := performHandlerRequest(t, http.MethodGet, "/accounts/1", nil, GetAccountHandler)
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body = %s", detail.Code, detail.Body.String())
	}
	for _, value := range []string{account.ClientSecret, account.OpenListToken, account.OpenListPassword, account.WebDAVPassword} {
		if !strings.Contains(detail.Body.String(), value) {
			t.Fatalf("account detail missing %q: %s", value, detail.Body.String())
		}
	}
	if strings.Contains(detail.Body.String(), `"DeletedAt"`) {
		t.Fatalf("account detail serialized the GORM model: %s", detail.Body.String())
	}
	var detailResponse struct {
		Data models.Account `json:"data"`
	}
	if err := json.Unmarshal(detail.Body.Bytes(), &detailResponse); err != nil {
		t.Fatal(err)
	}
	if detailResponse.Data.Version != account.Version {
		t.Fatalf("account detail version = %d, want %d", detailResponse.Data.Version, account.Version)
	}
	assertSensitiveResponseHeaders(t, detail)
}

func TestCreateAccountPlaybackModeDefaultsAndValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name        string
		accountType string
		mode        any
		wantStatus  int
		wantMode    string
	}{
		{name: "123 default", accountType: models.AccountType123Pan, wantStatus: http.StatusOK, wantMode: models.PlaybackModeRedirect},
		{name: "123 explicit proxy", accountType: models.AccountType123Pan, mode: models.PlaybackModeProxy, wantStatus: http.StatusOK, wantMode: models.PlaybackModeProxy},
		{name: "openlist default", accountType: models.AccountTypeOpenList, wantStatus: http.StatusOK, wantMode: models.PlaybackModeRedirect},
		{name: "openlist explicit proxy", accountType: models.AccountTypeOpenList, mode: models.PlaybackModeProxy, wantStatus: http.StatusOK, wantMode: models.PlaybackModeProxy},
		{name: "webdav default", accountType: models.AccountTypeWebDAV, wantStatus: http.StatusOK, wantMode: models.PlaybackModeProxy},
		{name: "webdav explicit redirect", accountType: models.AccountTypeWebDAV, mode: models.PlaybackModeRedirect, wantStatus: http.StatusOK, wantMode: models.PlaybackModeRedirect},
		{name: "invalid", accountType: models.AccountTypeWebDAV, mode: "direct", wantStatus: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openHandlerTestDB(t)
			payload := map[string]any{"Name": strings.ReplaceAll(tt.name, " ", "-"), "Type": tt.accountType}
			switch tt.accountType {
			case models.AccountType123Pan:
				payload["ClientID"] = "client-id"
				payload["ClientSecret"] = "client-secret"
			case models.AccountTypeOpenList:
				payload["OpenListURL"] = "https://list.example"
				payload["OpenListAuthMode"] = "token"
				payload["OpenListToken"] = "token"
			case models.AccountTypeWebDAV:
				payload["WebDAVURL"] = "https://dav.example"
			}
			if tt.mode != nil {
				payload["PlaybackMode"] = tt.mode
			}
			recorder := performHandlerRequest(t, http.MethodPost, "/accounts", payload, CreateAccountHandler)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
			if tt.wantStatus != http.StatusOK {
				return
			}
			var stored models.Account
			if err := db.First(&stored).Error; err != nil {
				t.Fatal(err)
			}
			if stored.PlaybackMode != tt.wantMode {
				t.Fatalf("stored mode = %q, want %q", stored.PlaybackMode, tt.wantMode)
			}
			if !strings.Contains(recorder.Body.String(), `"PlaybackMode":"`+tt.wantMode+`"`) {
				t.Fatalf("response missing normalized mode: %s", recorder.Body.String())
			}
		})
	}
}

func TestCreateOpenListAccountRejectsMissingOrInvalidAuthMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name     string
		authMode any
	}{
		{name: "missing"},
		{name: "invalid", authMode: "invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			openHandlerTestDB(t)
			payload := map[string]any{
				"Name":          "openlist-auth-mode",
				"Type":          models.AccountTypeOpenList,
				"OpenListURL":   "https://list.example",
				"OpenListToken": "token",
			}
			if test.authMode != nil {
				payload["OpenListAuthMode"] = test.authMode
			}
			recorder := performHandlerRequest(t, http.MethodPost, "/accounts", payload, CreateAccountHandler)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestUpdateAccountPlaybackModeOmissionAndValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{
		Name: "dav-mode", Type: models.AccountTypeWebDAV, WebDAVURL: "https://dav.example",
		PlaybackMode: models.PlaybackModeRedirect,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	omitted := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{
		"Name": "dav-renamed", "Version": account.Version,
	}, UpdateAccountHandler)
	if omitted.Code != http.StatusOK {
		t.Fatalf("omitted status = %d, body = %s", omitted.Code, omitted.Body.String())
	}
	var stored models.Account
	if err := db.First(&stored, account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.PlaybackMode != models.PlaybackModeRedirect {
		t.Fatalf("omitted update changed mode: %+v", stored)
	}
	updated := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{
		"PlaybackMode": models.PlaybackModeProxy,
		"Version":      stored.Version,
	}, UpdateAccountHandler)
	if updated.Code != http.StatusOK {
		t.Fatalf("updated status = %d, body = %s", updated.Code, updated.Body.String())
	}
	if err := db.First(&stored, account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.PlaybackMode != models.PlaybackModeProxy {
		t.Fatalf("explicit update did not change mode: %+v", stored)
	}
	invalid := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{
		"PlaybackMode": "unknown", "Version": stored.Version,
	}, UpdateAccountHandler)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d, body = %s", invalid.Code, invalid.Body.String())
	}
}

func TestAccountStreamSigningConfiguration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	payload := map[string]any{
		"Name": "signed-pan", "Type": models.AccountType123Pan,
		"ClientID": "client-id", "ClientSecret": "client-secret",
		"EnableStreamSign": true, "SignExpireHours": 87600,
	}
	recorder := performHandlerRequest(t, http.MethodPost, "/accounts", payload, CreateAccountHandler)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var stored models.Account
	if err := db.First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.EnableStreamSign || stored.SignExpireHours != 87600 {
		t.Fatalf("unexpected signing configuration: %+v", stored)
	}
	if !strings.Contains(recorder.Body.String(), `"EnableStreamSign":true`) || !strings.Contains(recorder.Body.String(), `"SignExpireHours":87600`) {
		t.Fatalf("response missing signing configuration: %s", recorder.Body.String())
	}
	updated := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{
		"EnableStreamSign": false,
		"SignExpireHours":  0,
		"Version":          stored.Version,
	}, UpdateAccountHandler)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", updated.Code, updated.Body.String())
	}
	if err := db.First(&stored, stored.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.EnableStreamSign || stored.SignExpireHours != 0 {
		t.Fatalf("signing configuration was not updated: %+v", stored)
	}

	for _, hours := range []int{-1, 87601} {
		invalid := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{
			"SignExpireHours": hours, "Version": stored.Version,
		}, UpdateAccountHandler)
		if invalid.Code != http.StatusBadRequest {
			t.Fatalf("hours %d status = %d, body = %s", hours, invalid.Code, invalid.Body.String())
		}
	}
}

func TestUpdateAccountWebDAVBindingChangeRetainsOmittedPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{
		Name:           "dav",
		Type:           models.AccountTypeWebDAV,
		WebDAVURL:      "https://old.example/dav",
		WebDAVUsername: "user",
		WebDAVPassword: "secret",
		StrmBaseURL:    "http://127.0.0.1:12398",
		CacheTTL:       30,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}

	recorder := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{
		"WebDAVURL": "https://new.example/dav",
		"Version":   account.Version,
	}, UpdateAccountHandler)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var stored models.Account
	if err := db.First(&stored, account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.WebDAVPassword != "secret" {
		t.Fatalf("password = %q, want retained secret", stored.WebDAVPassword)
	}
	if stored.WebDAVURL != "https://new.example/dav" {
		t.Fatalf("URL = %q", stored.WebDAVURL)
	}
}

func TestUpdateAnonymousWebDAVCanChangeBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{
		Name:           "anonymous-dav",
		Type:           models.AccountTypeWebDAV,
		WebDAVURL:      "https://old.example/dav",
		WebDAVUsername: "",
		WebDAVPassword: "",
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}

	recorder := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{
		"WebDAVURL": "https://new.example/dav",
		"Version":   account.Version,
	}, UpdateAccountHandler)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestUpdateWebDAVExplicitEmptyPasswordClearsStoredPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{
		Name:           "dav-retain",
		Type:           models.AccountTypeWebDAV,
		WebDAVURL:      "https://dav.example",
		WebDAVUsername: "user",
		WebDAVPassword: "secret",
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}

	recorder := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{
		"WebDAVPassword": "",
		"Version":        account.Version,
	}, UpdateAccountHandler)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var stored models.Account
	if err := db.First(&stored, account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.WebDAVPassword != "" {
		t.Fatalf("password = %q, want empty", stored.WebDAVPassword)
	}
}

func TestUpdateWebDAVEnablesRedirectModeWithStoredPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{
		Name:           "redirect-dav",
		Type:           models.AccountTypeWebDAV,
		WebDAVURL:      "https://files.example/dav",
		WebDAVUsername: "user",
		WebDAVPassword: "secret",
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}

	recorder := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{
		"PlaybackMode": models.PlaybackModeRedirect,
		"Version":      account.Version,
	}, UpdateAccountHandler)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var stored models.Account
	if err := db.First(&stored, account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.PlaybackMode != models.PlaybackModeRedirect || stored.WebDAVPassword != "secret" {
		t.Fatalf("unexpected stored account: %+v", stored)
	}
}

func TestCreateAnonymousWebDAVCanEnableRedirectMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	openHandlerTestDB(t)
	recorder := performHandlerRequest(t, http.MethodPost, "/accounts", map[string]any{
		"Name":           "anonymous-redirect",
		"Type":           models.AccountTypeWebDAV,
		"WebDAVURL":      "https://files.example/dav",
		"WebDAVUsername": " ",
		"WebDAVPassword": " ",
		"PlaybackMode":   models.PlaybackModeRedirect,
	}, CreateAccountHandler)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestAccountConnectionReturnsUnifiedSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	storedAccount := models.Account{
		Name:           "stored-account",
		Type:           models.AccountTypeWebDAV,
		WebDAVURL:      "https://stored.example/dav",
		WebDAVUsername: "stored-user",
		WebDAVPassword: "stored-password",
		PlaybackMode:   models.PlaybackModeProxy,
	}
	if err := db.Create(&storedAccount).Error; err != nil {
		t.Fatal(err)
	}
	var webDAVChecked bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dav/" {
			username, password, ok := r.BasicAuth()
			if !ok {
				w.Header().Set("WWW-Authenticate", `Basic realm="OpenList"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if username != "user" || password != "secret" || r.Method != "PROPFIND" {
				http.Error(w, "invalid WebDAV request", http.StatusUnauthorized)
				return
			}
			webDAVChecked = true
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?>
<d:multistatus xmlns:d="DAV:"><d:response><d:href>/dav/</d:href><d:propstat><d:prop><d:displayname>root</d:displayname><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	recorder := performHandlerRequest(t, http.MethodPost, "/accounts/test", map[string]any{
		"ID":               storedAccount.ID,
		"Name":             "connection-test",
		"Type":             models.AccountTypeWebDAV,
		"WebDAVURL":        server.URL + "/dav",
		"WebDAVUsername":   "user",
		"WebDAVPassword":   "secret",
		"PlaybackMode":     models.PlaybackModeRedirect,
		"EnableStreamSign": true,
		"SignExpireHours":  24,
	}, TestAccountConnectionHandler)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !webDAVChecked || !strings.Contains(recorder.Body.String(), "连接成功") || strings.Contains(recorder.Body.String(), "warning") {
		t.Fatalf("WebDAV checked=%v body=%s", webDAVChecked, recorder.Body.String())
	}
	var after models.Account
	if err := db.First(&after, storedAccount.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Name != storedAccount.Name || after.WebDAVURL != storedAccount.WebDAVURL || after.WebDAVPassword != storedAccount.WebDAVPassword {
		t.Fatalf("connection test modified stored account: %+v", after)
	}
}

func TestAccountConnectionDoesNotLoadStoredSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{
		Name: "stored-pan", Type: models.AccountType123Pan, ClientID: "client-id", ClientSecret: "stored-secret",
		PlaybackMode: models.PlaybackModeRedirect,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}

	recorder := performHandlerRequest(t, http.MethodPost, "/accounts/test", map[string]any{
		"ID": account.ID, "Name": "draft-pan", "Type": models.AccountType123Pan, "ClientID": account.ClientID,
	}, TestAccountConnectionHandler)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var after models.Account
	if err := db.First(&after, account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.ClientSecret != account.ClientSecret || after.Name != account.Name {
		t.Fatalf("connection test modified stored account: %+v", after)
	}
}

func TestAccountConnectionRejectsInvalidPlaybackMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	openHandlerTestDB(t)
	tests := []map[string]any{
		{"Name": "pan-mode", "Type": models.AccountType123Pan, "ClientID": "id", "ClientSecret": "secret"},
		{"Name": "openlist-mode", "Type": models.AccountTypeOpenList, "OpenListURL": "https://list.invalid", "OpenListAuthMode": "token", "OpenListToken": "token"},
		{"Name": "webdav-mode", "Type": models.AccountTypeWebDAV, "WebDAVURL": "https://dav.invalid"},
	}
	for _, payload := range tests {
		payload["PlaybackMode"] = "invalid"
		invalid := performHandlerRequest(t, http.MethodPost, "/accounts/test", payload, TestAccountConnectionHandler)
		if invalid.Code != http.StatusBadRequest {
			t.Fatalf("type %s status = %d, body = %s", payload["Type"], invalid.Code, invalid.Body.String())
		}
	}
}

func TestUpdateTaskLocalPathClearsHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{Name: "pan", Type: models.AccountType123Pan, ClientID: "id", ClientSecret: "secret"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	oldRoot := filepath.Join(t.TempDir(), "old")
	newRoot := filepath.Join(t.TempDir(), "new")
	task := models.Task{Name: "task", AccountID: account.ID, SourceFolderID: "0", LocalPath: oldRoot, Cron: "0 * * * *", Enabled: false, Threads: 1}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.TaskFile{TaskID: task.ID, FilePath: filepath.Join(oldRoot, "movie.strm")}).Error; err != nil {
		t.Fatal(err)
	}

	recorder := performHandlerRequest(t, http.MethodPut, "/tasks/1", map[string]any{"LocalPath": newRoot}, UpdateTaskHandler)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var count int64
	if err := db.Model(&models.TaskFile{}).Where("task_id = ?", task.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("task history count = %d, want 0", count)
	}
}

func TestUpdateAccountClientIDChangeRetainsOmittedSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{Name: "pan", Type: models.AccountType123Pan, ClientID: "old-id", ClientSecret: "old-secret"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}

	recorder := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{
		"ClientID": "new-id", "Version": account.Version,
	}, UpdateAccountHandler)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var stored models.Account
	if err := db.First(&stored, account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ClientID != "new-id" || stored.ClientSecret != "old-secret" {
		t.Fatalf("stored credentials = ID %q secret %q", stored.ClientID, stored.ClientSecret)
	}
}

func TestUpdateAccountRetainsBoundSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{Name: "pan", Type: models.AccountType123Pan, ClientID: "id", ClientSecret: "secret"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}

	recorder := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{
		"Name": "renamed", "Version": account.Version,
	}, UpdateAccountHandler)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var stored models.Account
	if err := db.First(&stored, account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ClientSecret != "secret" {
		t.Fatalf("secret = %q, want retained secret", stored.ClientSecret)
	}
}

func TestUpdateAccountAcceptsCompleteFormSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{Name: "pan", Type: models.AccountType123Pan, ClientID: "id", ClientSecret: "old-secret"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}

	recorder := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{
		"Name": "renamed", "ClientSecret": "current-form-secret", "Version": account.Version,
	}, UpdateAccountHandler)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var stored models.Account
	if err := db.First(&stored, account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Name != "renamed" || stored.ClientSecret != "current-form-secret" {
		t.Fatalf("complete form was not saved: %+v", stored)
	}
}

func TestUpdateAccountRejectsStaleCompleteForm(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{
		Name:                "pan",
		Type:                models.AccountType123Pan,
		ClientID:            "old-id",
		ClientSecret:        "old-secret",
		StrmBaseURL:         "https://old.example",
		PlaybackMode:        models.PlaybackModeRedirect,
		EnableStreamSign:    false,
		SignExpireHours:     12,
		CacheTTL:            30,
		CustomCachePolicies: "/old/*:30",
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}

	type detailResponse struct {
		Data models.Account `json:"data"`
	}
	readView := func() detailResponse {
		t.Helper()
		response := performHandlerRequest(t, http.MethodGet, "/accounts/1", nil, GetAccountHandler)
		if response.Code != http.StatusOK {
			t.Fatalf("detail status = %d, body = %s", response.Code, response.Body.String())
		}
		var view detailResponse
		if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}
		return view
	}
	firstView := readView()
	secondView := readView()
	if firstView.Data.Version != account.Version || secondView.Data.Version != account.Version {
		t.Fatalf("edit view versions = %d and %d, want %d", firstView.Data.Version, secondView.Data.Version, account.Version)
	}

	firstUpdate := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{
		"Name":                "first-save",
		"Type":                models.AccountType123Pan,
		"ClientID":            "new-id",
		"ClientSecret":        "new-secret",
		"OpenListURL":         "",
		"OpenListAuthMode":    "",
		"OpenListToken":       "",
		"OpenListUsername":    "",
		"OpenListPassword":    "",
		"WebDAVURL":           "",
		"WebDAVUsername":      "",
		"WebDAVPassword":      "",
		"StrmBaseURL":         "https://new.example",
		"PlaybackMode":        models.PlaybackModeProxy,
		"EnableStreamSign":    true,
		"SignExpireHours":     24,
		"CacheTTL":            45,
		"CustomCachePolicies": "/new/*:60",
		"Version":             firstView.Data.Version,
	}, UpdateAccountHandler)
	if firstUpdate.Code != http.StatusOK {
		t.Fatalf("first update status = %d, body = %s", firstUpdate.Code, firstUpdate.Body.String())
	}
	var firstUpdateResponse detailResponse
	if err := json.Unmarshal(firstUpdate.Body.Bytes(), &firstUpdateResponse); err != nil {
		t.Fatal(err)
	}
	if firstUpdateResponse.Data.Version != account.Version+1 {
		t.Fatalf("first update version = %d, want %d", firstUpdateResponse.Data.Version, account.Version+1)
	}

	staleUpdate := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{
		"Name":                secondView.Data.Name,
		"Type":                secondView.Data.Type,
		"ClientID":            secondView.Data.ClientID,
		"ClientSecret":        secondView.Data.ClientSecret,
		"OpenListURL":         secondView.Data.OpenListURL,
		"OpenListAuthMode":    secondView.Data.OpenListAuthMode,
		"OpenListToken":       secondView.Data.OpenListToken,
		"OpenListUsername":    secondView.Data.OpenListUsername,
		"OpenListPassword":    secondView.Data.OpenListPassword,
		"WebDAVURL":           secondView.Data.WebDAVURL,
		"WebDAVUsername":      secondView.Data.WebDAVUsername,
		"WebDAVPassword":      secondView.Data.WebDAVPassword,
		"StrmBaseURL":         secondView.Data.StrmBaseURL,
		"PlaybackMode":        secondView.Data.PlaybackMode,
		"EnableStreamSign":    secondView.Data.EnableStreamSign,
		"SignExpireHours":     secondView.Data.SignExpireHours,
		"CacheTTL":            secondView.Data.CacheTTL,
		"CustomCachePolicies": secondView.Data.CustomCachePolicies,
		"Version":             secondView.Data.Version,
	}, UpdateAccountHandler)
	if staleUpdate.Code != http.StatusConflict {
		t.Fatalf("stale update status = %d, body = %s", staleUpdate.Code, staleUpdate.Body.String())
	}

	var stored models.Account
	if err := db.First(&stored, account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Name != "first-save" || stored.ClientID != "new-id" || stored.ClientSecret != "new-secret" ||
		stored.StrmBaseURL != "https://new.example" || stored.PlaybackMode != models.PlaybackModeProxy ||
		!stored.EnableStreamSign || stored.SignExpireHours != 24 || stored.CacheTTL != 45 ||
		stored.CustomCachePolicies != "/new/*:60" || stored.Version != account.Version+1 {
		t.Fatalf("stale form overwrote first update: %+v", stored)
	}
}

func TestUpdateAccountRejectsMissingVersionWithoutChangingRecord(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{
		Name: "pan", Type: models.AccountType123Pan, ClientID: "id", ClientSecret: "secret",
		PlaybackMode: models.PlaybackModeRedirect,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}

	response := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{
		"Name": "renamed", "ClientSecret": account.ClientSecret,
	}, UpdateAccountHandler)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var stored models.Account
	if err := db.First(&stored, account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Name != account.Name || stored.ClientSecret != account.ClientSecret || stored.Version != account.Version {
		t.Fatalf("missing-version update changed stored account: %+v", stored)
	}
}

func TestUpdateAccountRejectsExplicitEmptyRequiredSecretsWithoutChangingStoredValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name    string
		account models.Account
		field   string
	}{
		{
			name: "123pan", field: "ClientSecret",
			account: models.Account{Name: "pan", Type: models.AccountType123Pan, ClientID: "id", ClientSecret: "old-secret"},
		},
		{
			name: "openlist token", field: "OpenListToken",
			account: models.Account{Name: "openlist", Type: models.AccountTypeOpenList, OpenListURL: "https://list.example", OpenListAuthMode: "token", OpenListToken: "old-token"},
		},
		{
			name: "openlist password", field: "OpenListPassword",
			account: models.Account{Name: "openlist-password", Type: models.AccountTypeOpenList, OpenListURL: "https://list.example", OpenListAuthMode: "password", OpenListUsername: "user", OpenListPassword: "old-password"},
		},
	} {
		for _, value := range []struct {
			name  string
			value string
		}{
			{name: "empty", value: ""},
			{name: "whitespace", value: "   "},
		} {
			t.Run(test.name+"/"+value.name, func(t *testing.T) {
				db := openHandlerTestDB(t)
				account := test.account
				if err := db.Create(&account).Error; err != nil {
					t.Fatal(err)
				}
				recorder := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{
					"Name": "should-not-save", test.field: value.value, "Version": account.Version,
				}, UpdateAccountHandler)
				if recorder.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
				}
				var stored models.Account
				if err := db.First(&stored, account.ID).Error; err != nil {
					t.Fatal(err)
				}
				if stored.Name != account.Name || stored.Version != account.Version {
					t.Fatalf("invalid update changed stored account: %+v", stored)
				}
				switch test.field {
				case "ClientSecret":
					if stored.ClientSecret != account.ClientSecret {
						t.Fatalf("client secret changed to %q", stored.ClientSecret)
					}
				case "OpenListToken":
					if stored.OpenListToken != account.OpenListToken {
						t.Fatalf("token changed to %q", stored.OpenListToken)
					}
				case "OpenListPassword":
					if stored.OpenListPassword != account.OpenListPassword {
						t.Fatalf("password changed to %q", stored.OpenListPassword)
					}
				}
			})
		}
	}
}

func TestUpdateAccountOpenListClearsInactiveCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{
		Name:                "openlist",
		Type:                models.AccountTypeOpenList,
		OpenListURL:         "https://openlist.example",
		OpenListAuthMode:    "password",
		OpenListUsername:    "user",
		OpenListPassword:    "password",
		OpenListToken:       "stale-token",
		ClientID:            "stale-id",
		ClientSecret:        "stale-secret",
		WebDAVPassword:      "stale-password",
		CustomCachePolicies: "",
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}

	recorder := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{
		"Name": "openlist-renamed", "Version": account.Version,
	}, UpdateAccountHandler)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var stored models.Account
	if err := db.First(&stored, account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.OpenListToken != "" || stored.ClientID != "" || stored.ClientSecret != "" || stored.WebDAVPassword != "" {
		t.Fatalf("inactive credentials were retained: %+v", stored)
	}
	if stored.OpenListPassword != "password" {
		t.Fatalf("active password = %q", stored.OpenListPassword)
	}
}

func TestUpdateAccountTypeChangeClearsOldCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{Name: "pan", Type: models.AccountType123Pan, ClientID: "id", ClientSecret: "secret"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}

	recorder := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{
		"Type":           models.AccountTypeWebDAV,
		"WebDAVURL":      "https://dav.example",
		"WebDAVUsername": "user",
		"WebDAVPassword": "password",
		"Version":        account.Version,
	}, UpdateAccountHandler)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var stored models.Account
	if err := db.First(&stored, account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Type != models.AccountTypeWebDAV || stored.ClientID != "" || stored.ClientSecret != "" {
		t.Fatalf("old credentials retained: %+v", stored)
	}
	if stored.WebDAVPassword != "password" {
		t.Fatalf("new password = %q", stored.WebDAVPassword)
	}
}

func TestUpdateAccountTypeBlockedByTask(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{Name: "pan", Type: models.AccountType123Pan, ClientID: "id", ClientSecret: "secret"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	task := models.Task{Name: "task", AccountID: account.ID, SourceFolderID: "0", LocalPath: filepath.Join(t.TempDir(), "media"), Cron: "0 * * * *", Enabled: true, Threads: 1}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	recorder := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{
		"Type":             models.AccountTypeOpenList,
		"OpenListURL":      "https://openlist.example",
		"OpenListAuthMode": "token",
		"OpenListToken":    "token",
		"Version":          account.Version,
	}, UpdateAccountHandler)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestStopTaskNotRunningReturnsConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{Name: "pan", Type: models.AccountType123Pan, ClientID: "id", ClientSecret: "secret"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	task := models.Task{Name: "task", AccountID: account.ID, SourceFolderID: "0", LocalPath: filepath.Join(t.TempDir(), "media"), Cron: "0 * * * *", Enabled: true, Threads: 1}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	recorder := performHandlerRequest(t, http.MethodPost, "/tasks/1", nil, StopTaskHandler)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestExecuteTaskRejectsMalformedID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := performHandlerRequest(t, http.MethodPost, "/tasks/1abc", nil, ExecuteTaskHandler)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestAccountNameConflictReturnsStableMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	first := models.Account{Name: "first", Type: models.AccountType123Pan, ClientID: "id-1", ClientSecret: "secret-1"}
	second := models.Account{Name: "second", Type: models.AccountType123Pan, ClientID: "id-2", ClientSecret: "secret-2"}
	if err := db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}

	recorder := performHandlerRequest(t, http.MethodPut, "/accounts/2", map[string]any{
		"Name": "first", "Version": second.Version,
	}, UpdateAccountHandler)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "账户名称已存在") || strings.Contains(strings.ToLower(recorder.Body.String()), "constraint") {
		t.Fatalf("unstable conflict response: %s", recorder.Body.String())
	}
}
