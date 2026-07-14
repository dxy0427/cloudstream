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
	"time"

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

func TestRevealAccountSecretIsExplicitAndNoStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{
		Name:           "visible-dav",
		Type:           models.AccountTypeWebDAV,
		WebDAVURL:      "https://dav.example",
		WebDAVUsername: "user",
		WebDAVPassword: "saved-password",
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}

	listed := performHandlerRequest(t, http.MethodGet, "/accounts", nil, ListAccountsHandler)
	if listed.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listed.Code, listed.Body.String())
	}
	if strings.Contains(listed.Body.String(), "saved-password") {
		t.Fatalf("account list leaked password: %s", listed.Body.String())
	}

	revealed := performHandlerRequest(t, http.MethodPost, "/accounts/1", map[string]any{
		"field": "webdav_password", "updatedAt": account.UpdatedAt.Format(time.RFC3339Nano),
		"accountType": models.AccountTypeWebDAV, "webdavUrl": "https://dav.example/", "webdavUsername": " user ",
	}, RevealAccountSecretHandler)
	if revealed.Code != http.StatusOK {
		t.Fatalf("reveal status = %d, body = %s", revealed.Code, revealed.Body.String())
	}
	if !strings.Contains(revealed.Body.String(), "saved-password") {
		t.Fatalf("reveal response missing password: %s", revealed.Body.String())
	}
	if cacheControl := revealed.Header().Get("Cache-Control"); !strings.Contains(cacheControl, "no-store") {
		t.Fatalf("Cache-Control = %q", cacheControl)
	}
	if revealed.Header().Get("Pragma") != "no-cache" || revealed.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing reveal safety headers: %v", revealed.Header())
	}

	wrongField := performHandlerRequest(t, http.MethodPost, "/accounts/1", map[string]any{
		"field": "client_secret", "updatedAt": account.UpdatedAt.Format(time.RFC3339Nano),
		"accountType": models.AccountTypeWebDAV, "clientId": "not-applicable",
	}, RevealAccountSecretHandler)
	if wrongField.Code != http.StatusConflict {
		t.Fatalf("wrong-field status = %d, body = %s", wrongField.Code, wrongField.Body.String())
	}
	stale := performHandlerRequest(t, http.MethodPost, "/accounts/1", map[string]any{
		"field": "webdav_password", "updatedAt": time.Now().Add(-time.Hour).Format(time.RFC3339Nano),
		"accountType": models.AccountTypeWebDAV, "webdavUrl": account.WebDAVURL, "webdavUsername": account.WebDAVUsername,
	}, RevealAccountSecretHandler)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale status = %d, body = %s", stale.Code, stale.Body.String())
	}
}

func TestRevealOpenListUsesOnlyActiveCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{
		Name:             "openlist-token",
		Type:             models.AccountTypeOpenList,
		OpenListURL:      "https://list.example",
		OpenListAuthMode: "token",
		OpenListToken:    "saved-token",
		OpenListPassword: "inactive-password",
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}

	token := performHandlerRequest(t, http.MethodPost, "/accounts/1", map[string]any{
		"field": "openlist_token", "updatedAt": account.UpdatedAt.Format(time.RFC3339Nano),
		"accountType": models.AccountTypeOpenList, "openListAuthMode": "token", "openListUrl": "https://list.example/",
	}, RevealAccountSecretHandler)
	if token.Code != http.StatusOK || !strings.Contains(token.Body.String(), "saved-token") {
		t.Fatalf("token reveal status = %d, body = %s", token.Code, token.Body.String())
	}
	password := performHandlerRequest(t, http.MethodPost, "/accounts/1", map[string]any{
		"field": "openlist_password", "updatedAt": account.UpdatedAt.Format(time.RFC3339Nano),
		"accountType": models.AccountTypeOpenList, "openListAuthMode": "password", "openListUrl": account.OpenListURL, "openListUsername": "",
	}, RevealAccountSecretHandler)
	if password.Code != http.StatusConflict {
		t.Fatalf("inactive password status = %d, body = %s", password.Code, password.Body.String())
	}
}

func TestRevealOpenListPasswordRequiresCompleteBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{
		Name:             "openlist-password",
		Type:             models.AccountTypeOpenList,
		OpenListURL:      "https://list.example/dav",
		OpenListAuthMode: "password",
		OpenListUsername: "user",
		OpenListPassword: "saved-password",
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}

	payload := map[string]any{
		"field": "openlist_password", "updatedAt": account.UpdatedAt.Format(time.RFC3339Nano),
		"accountType": models.AccountTypeOpenList, "openListAuthMode": "password",
		"openListUrl": "https://list.example/dav/", "openListUsername": " user ",
	}
	revealed := performHandlerRequest(t, http.MethodPost, "/accounts/1", payload, RevealAccountSecretHandler)
	if revealed.Code != http.StatusOK || !strings.Contains(revealed.Body.String(), "saved-password") {
		t.Fatalf("reveal status = %d, body = %s", revealed.Code, revealed.Body.String())
	}

	delete(payload, "openListUsername")
	missing := performHandlerRequest(t, http.MethodPost, "/accounts/1", payload, RevealAccountSecretHandler)
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing username status = %d, body = %s", missing.Code, missing.Body.String())
	}
	payload["openListUsername"] = "other-user"
	changed := performHandlerRequest(t, http.MethodPost, "/accounts/1", payload, RevealAccountSecretHandler)
	if changed.Code != http.StatusConflict {
		t.Fatalf("changed username status = %d, body = %s", changed.Code, changed.Body.String())
	}
}

func TestRevealAccountSecretRejectsMissingOrChangedBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{
		Name: "bound-pan", Type: models.AccountType123Pan, ClientID: "client-id", ClientSecret: "saved-secret",
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	base := map[string]any{
		"field": "client_secret", "updatedAt": account.UpdatedAt.Format(time.RFC3339Nano), "accountType": models.AccountType123Pan,
	}
	missing := performHandlerRequest(t, http.MethodPost, "/accounts/1", base, RevealAccountSecretHandler)
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing binding status = %d, body = %s", missing.Code, missing.Body.String())
	}
	base["clientId"] = "changed-id"
	changed := performHandlerRequest(t, http.MethodPost, "/accounts/1", base, RevealAccountSecretHandler)
	if changed.Code != http.StatusConflict {
		t.Fatalf("changed binding status = %d, body = %s", changed.Code, changed.Body.String())
	}
	base["clientId"] = account.ClientID
	base["accountType"] = models.AccountTypeWebDAV
	wrongType := performHandlerRequest(t, http.MethodPost, "/accounts/1", base, RevealAccountSecretHandler)
	if wrongType.Code != http.StatusConflict {
		t.Fatalf("wrong type status = %d, body = %s", wrongType.Code, wrongType.Body.String())
	}
}

func TestCreateAccountWebDAVPlaybackModeCompatibility(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		fields     map[string]any
		wantStatus int
		wantMode   string
		wantLegacy bool
	}{
		{name: "default", fields: map[string]any{}, wantStatus: http.StatusOK, wantMode: models.WebDAVPlaybackModeProxy},
		{name: "new redirect", fields: map[string]any{"WebDAVPlaybackMode": models.WebDAVPlaybackModeUpstreamRedirect}, wantStatus: http.StatusOK, wantMode: models.WebDAVPlaybackModeUpstreamRedirect, wantLegacy: true},
		{name: "legacy redirect", fields: map[string]any{"WebDAVDirectLink": true}, wantStatus: http.StatusOK, wantMode: models.WebDAVPlaybackModeUpstreamRedirect, wantLegacy: true},
		{name: "consistent", fields: map[string]any{"WebDAVPlaybackMode": models.WebDAVPlaybackModeProxy, "WebDAVDirectLink": false}, wantStatus: http.StatusOK, wantMode: models.WebDAVPlaybackModeProxy},
		{name: "conflict", fields: map[string]any{"WebDAVPlaybackMode": models.WebDAVPlaybackModeProxy, "WebDAVDirectLink": true}, wantStatus: http.StatusBadRequest},
		{name: "invalid", fields: map[string]any{"WebDAVPlaybackMode": "direct"}, wantStatus: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openHandlerTestDB(t)
			payload := map[string]any{"Name": "dav-" + strings.ReplaceAll(tt.name, " ", "-"), "Type": models.AccountTypeWebDAV, "WebDAVURL": "https://dav.example"}
			for key, value := range tt.fields {
				payload[key] = value
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
			if stored.WebDAVPlaybackMode != tt.wantMode || stored.WebDAVDirectLink != tt.wantLegacy {
				t.Fatalf("stored mode = %q legacy = %v", stored.WebDAVPlaybackMode, stored.WebDAVDirectLink)
			}
			if !strings.Contains(recorder.Body.String(), `"WebDAVPlaybackMode":"`+tt.wantMode+`"`) {
				t.Fatalf("response missing normalized mode: %s", recorder.Body.String())
			}
		})
	}
}

func TestUpdateAccountPlaybackModeOmissionAndValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{
		Name: "dav-mode", Type: models.AccountTypeWebDAV, WebDAVURL: "https://dav.example",
		WebDAVPlaybackMode: models.WebDAVPlaybackModeUpstreamRedirect, WebDAVDirectLink: true,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	omitted := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{"Name": "dav-renamed"}, UpdateAccountHandler)
	if omitted.Code != http.StatusOK {
		t.Fatalf("omitted status = %d, body = %s", omitted.Code, omitted.Body.String())
	}
	var stored models.Account
	if err := db.First(&stored, account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.WebDAVPlaybackMode != models.WebDAVPlaybackModeUpstreamRedirect || !stored.WebDAVDirectLink {
		t.Fatalf("omitted update changed mode: %+v", stored)
	}
	conflict := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{
		"WebDAVPlaybackMode": models.WebDAVPlaybackModeUpstreamRedirect, "WebDAVDirectLink": false,
	}, UpdateAccountHandler)
	if conflict.Code != http.StatusBadRequest {
		t.Fatalf("conflict status = %d, body = %s", conflict.Code, conflict.Body.String())
	}
	invalid := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{"WebDAVPlaybackMode": "unknown"}, UpdateAccountHandler)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d, body = %s", invalid.Code, invalid.Body.String())
	}
}

func TestUpdateAccountWebDAVBindingChangeClearsPassword(t *testing.T) {
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
	}, UpdateAccountHandler)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestUpdateWebDAVCanClearStoredPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{
		Name:           "dav-clear",
		Type:           models.AccountTypeWebDAV,
		WebDAVURL:      "https://dav.example",
		WebDAVUsername: "user",
		WebDAVPassword: "secret",
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}

	recorder := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{
		"ClearWebDAVPassword": true,
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

func TestMergeStoredWebDAVSecretsHonorsExplicitClear(t *testing.T) {
	account := models.Account{Type: models.AccountTypeWebDAV, WebDAVURL: "https://dav.example", WebDAVUsername: "user"}
	stored := account
	stored.WebDAVPassword = "secret"
	if ok, msg := mergeStoredAccountSecretsForUpdate(&account, stored, true); !ok {
		t.Fatalf("clear rejected: %s", msg)
	}
	if account.WebDAVPassword != "" {
		t.Fatalf("password = %q, want empty", account.WebDAVPassword)
	}
}

func TestUpdateWebDAVEnablesRedirectModeWithStoredPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{
		Name:           "direct-dav",
		Type:           models.AccountTypeWebDAV,
		WebDAVURL:      "https://files.example/dav",
		WebDAVUsername: "user",
		WebDAVPassword: "secret",
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}

	recorder := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{
		"WebDAVDirectLink": true,
	}, UpdateAccountHandler)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var stored models.Account
	if err := db.First(&stored, account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.WebDAVDirectLink || stored.WebDAVPassword != "secret" {
		t.Fatalf("unexpected stored account: %+v", stored)
	}
}

func TestCreateAnonymousWebDAVCanEnableRedirectMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	openHandlerTestDB(t)
	recorder := performHandlerRequest(t, http.MethodPost, "/accounts", map[string]any{
		"Name":             "invalid-direct",
		"Type":             models.AccountTypeWebDAV,
		"WebDAVURL":        "https://files.example/dav",
		"WebDAVUsername":   " ",
		"WebDAVPassword":   " ",
		"WebDAVDirectLink": true,
	}, CreateAccountHandler)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestAccountConnectionWebDAVRedirectModeWarnsThatFileRedirectIsRequired(t *testing.T) {
	gin.SetMode(gin.TestMode)
	openHandlerTestDB(t)
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
		"Name":             "direct-test",
		"Type":             models.AccountTypeWebDAV,
		"WebDAVURL":        server.URL + "/dav",
		"WebDAVUsername":   "user",
		"WebDAVPassword":   "secret",
		"WebDAVDirectLink": true,
	}, TestAccountConnectionHandler)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !webDAVChecked || !strings.Contains(recorder.Body.String(), "文件请求实际返回 3xx") {
		t.Fatalf("WebDAV checked=%v body=%s", webDAVChecked, recorder.Body.String())
	}
}

func TestAccountConnectionRejectsInvalidOrConflictingPlaybackMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	openHandlerTestDB(t)
	base := map[string]any{
		"Name": "mode-validation", "Type": models.AccountTypeWebDAV, "WebDAVURL": "https://dav.invalid",
	}

	base["WebDAVPlaybackMode"] = "invalid"
	invalid := performHandlerRequest(t, http.MethodPost, "/accounts/test", base, TestAccountConnectionHandler)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d, body = %s", invalid.Code, invalid.Body.String())
	}

	base["WebDAVPlaybackMode"] = models.WebDAVPlaybackModeProxy
	base["WebDAVDirectLink"] = true
	conflict := performHandlerRequest(t, http.MethodPost, "/accounts/test", base, TestAccountConnectionHandler)
	if conflict.Code != http.StatusBadRequest {
		t.Fatalf("conflict status = %d, body = %s", conflict.Code, conflict.Body.String())
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

func TestUpdateAccountClientIDChangeRequiresNewSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{Name: "pan", Type: models.AccountType123Pan, ClientID: "old-id", ClientSecret: "old-secret"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}

	recorder := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{"ClientID": "new-id"}, UpdateAccountHandler)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var stored models.Account
	if err := db.First(&stored, account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ClientID != "old-id" || stored.ClientSecret != "old-secret" {
		t.Fatalf("stored credentials changed: ID=%q secret=%q", stored.ClientID, stored.ClientSecret)
	}
}

func TestUpdateAccountRetainsBoundSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	account := models.Account{Name: "pan", Type: models.AccountType123Pan, ClientID: "id", ClientSecret: "secret"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}

	recorder := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{"Name": "renamed"}, UpdateAccountHandler)
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

	recorder := performHandlerRequest(t, http.MethodPut, "/accounts/1", map[string]any{"Name": "openlist-renamed"}, UpdateAccountHandler)
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

	recorder := performHandlerRequest(t, http.MethodPut, "/accounts/2", map[string]any{"Name": "first"}, UpdateAccountHandler)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "账户名称已存在") || strings.Contains(strings.ToLower(recorder.Body.String()), "constraint") {
		t.Fatalf("unstable conflict response: %s", recorder.Body.String())
	}
}
