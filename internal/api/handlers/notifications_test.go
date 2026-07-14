package handlers

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func openNotificationTestDB(t *testing.T) (models.User, func(string, string, any, gin.HandlerFunc) *httptest.ResponseRecorder) {
	t.Helper()
	db := openHandlerTestDB(t)
	if err := db.AutoMigrate(&models.User{}, &models.Notification{}); err != nil {
		t.Fatal(err)
	}
	user := models.User{Username: "admin", PasswordHash: "hash"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	request := func(method, requestPath string, payload any, handler gin.HandlerFunc) *httptest.ResponseRecorder {
		recorder := performHandlerRequest(t, method, requestPath, payload, func(c *gin.Context) {
			c.Set("username", user.Username)
			handler(c)
		})
		return recorder
	}
	return user, request
}

func TestNotificationCRUDAndSecretRedaction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	user, request := openNotificationTestDB(t)

	created := request(http.MethodPost, "/notifications", map[string]any{
		"Name":             "ops-webhook",
		"Type":             models.NotifyTypeWebhook,
		"WebhookURL":       "https://hooks.example/secret",
		"Enabled":          false,
		"NotifyOnComplete": false,
		"NotifyOnError":    true,
		"NotifyOnStop":     false,
		"NotifyOnManual":   false,
	}, CreateNotificationHandler)
	if created.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}

	var stored models.Notification
	if err := databaseNotificationDB().Where("user_id = ?", user.ID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Enabled || stored.NotifyOnComplete || !stored.NotifyOnError || stored.NotifyOnStop || stored.NotifyOnManual {
		t.Fatalf("explicit booleans were not preserved: %+v", stored)
	}
	if stored.WebhookURL != "https://hooks.example/secret" {
		t.Fatalf("webhook URL = %q", stored.WebhookURL)
	}

	listed := request(http.MethodGet, "/notifications", nil, ListNotificationsHandler)
	if listed.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	if listed.Body.String() == "" || containsSecret(listed.Body.Bytes(), "https://hooks.example/secret") {
		t.Fatalf("list leaked secret: %s", listed.Body.String())
	}
	var listResponse struct {
		Data []struct {
			ID            uint `json:"ID"`
			Version       int  `json:"Version"`
			HasWebhookURL bool `json:"HasWebhookURL"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &listResponse); err != nil {
		t.Fatal(err)
	}
	if len(listResponse.Data) != 1 || !listResponse.Data[0].HasWebhookURL || listResponse.Data[0].Version != 1 {
		t.Fatalf("unexpected list response: %s", listed.Body.String())
	}

	updated := request(http.MethodPut, "/notifications/1", map[string]any{
		"Name":    "ops-renamed",
		"Enabled": true,
		"Version": stored.Version,
	}, UpdateNotificationHandler)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	if err := databaseNotificationDB().First(&stored, stored.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Name != "ops-renamed" || !stored.Enabled || stored.WebhookURL != "https://hooks.example/secret" {
		t.Fatalf("update did not preserve secret: %+v", stored)
	}

	deleted := request(http.MethodDelete, "/notifications/1?version=2", nil, DeleteNotificationHandler)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
}

func TestNotificationOwnershipAndTypeChange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	user, request := openNotificationTestDB(t)
	other := models.User{Username: "other", PasswordHash: "hash"}
	if err := databaseNotificationDB().Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	notification := models.Notification{UserID: other.ID, Name: "other-target", Type: models.NotifyTypeWebhook, WebhookURL: "https://hooks.example/other"}
	if err := databaseNotificationDB().Create(&notification).Error; err != nil {
		t.Fatal(err)
	}

	response := request(http.MethodPut, "/notifications/1", map[string]any{"Name": "stolen"}, UpdateNotificationHandler)
	if response.Code != http.StatusNotFound {
		t.Fatalf("cross-user update status=%d body=%s", response.Code, response.Body.String())
	}

	owned := models.Notification{UserID: user.ID, Name: "owned", Type: models.NotifyTypeWebhook, WebhookURL: "https://hooks.example/owned"}
	if err := databaseNotificationDB().Create(&owned).Error; err != nil {
		t.Fatal(err)
	}
	response = request(http.MethodPut, "/notifications/2", map[string]any{
		"Type":           models.NotifyTypeTelegram,
		"TelegramChatID": "123",
		"Version":        owned.Version,
	}, UpdateNotificationHandler)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("type change without token status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestNotificationTestUsesStoredSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	user, request := openNotificationTestDB(t)
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer webhook.Close()
	notification := models.Notification{UserID: user.ID, Name: "stored", Type: models.NotifyTypeWebhook, WebhookURL: webhook.URL, Enabled: true}
	if err := databaseNotificationDB().Create(&notification).Error; err != nil {
		t.Fatal(err)
	}

	response := request(http.MethodPost, "/notifications/test", map[string]any{"ID": notification.ID, "Version": notification.Version}, TestNotificationHandler)
	if response.Code != http.StatusOK {
		t.Fatalf("test status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestNotificationRejectsStaleVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	user, request := openNotificationTestDB(t)
	notification := models.Notification{UserID: user.ID, Name: "versioned", Type: models.NotifyTypeWebhook, WebhookURL: "https://hooks.example/versioned", Version: 2}
	if err := databaseNotificationDB().Create(&notification).Error; err != nil {
		t.Fatal(err)
	}

	updated := request(http.MethodPut, "/notifications/1", map[string]any{"Name": "stale", "Version": 1}, UpdateNotificationHandler)
	if updated.Code != http.StatusConflict {
		t.Fatalf("stale update status=%d body=%s", updated.Code, updated.Body.String())
	}
	tested := request(http.MethodPost, "/notifications/test", map[string]any{"ID": notification.ID, "Version": 1}, TestNotificationHandler)
	if tested.Code != http.StatusConflict {
		t.Fatalf("stale test status=%d body=%s", tested.Code, tested.Body.String())
	}
	deleted := request(http.MethodDelete, "/notifications/1?version=1", nil, DeleteNotificationHandler)
	if deleted.Code != http.StatusConflict {
		t.Fatalf("stale delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
}

func databaseNotificationDB() *gorm.DB {
	return database.DB
}

func containsSecret(body []byte, secret string) bool {
	return strings.Contains(string(body), secret)
}
