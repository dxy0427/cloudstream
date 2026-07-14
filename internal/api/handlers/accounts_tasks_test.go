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
	if index := strings.LastIndex(path, "/"); index >= 0 {
		ctx.Params = gin.Params{{Key: "id", Value: path[index+1:]}}
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
