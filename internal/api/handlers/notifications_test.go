package handlers

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"net/http"
	"net/http/httptest"
	"strconv"
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
		return performHandlerRequest(t, method, requestPath, payload, func(c *gin.Context) {
			c.Set("username", user.Username)
			handler(c)
		})
	}
	return user, request
}

func TestNotificationCRUDListRedactionAndDetail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	user, request := openNotificationTestDB(t)
	secret := "https://hooks.example/saved-secret"

	created := request(http.MethodPost, "/notifications", map[string]any{
		"Name":             "ops-webhook",
		"Type":             models.NotifyTypeWebhook,
		"WebhookURL":       secret,
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

	listed := request(http.MethodGet, "/notifications", nil, ListNotificationsHandler)
	if listed.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	if strings.Contains(listed.Body.String(), secret) || strings.Contains(listed.Body.String(), `"WebhookURL"`) ||
		strings.Contains(listed.Body.String(), `"TelegramToken"`) || strings.Contains(listed.Body.String(), `"HasWebhookURL"`) {
		t.Fatalf("notification list leaked secret data: %s", listed.Body.String())
	}

	detail := request(http.MethodGet, "/notifications/1", nil, GetNotificationHandler)
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), secret) {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	if strings.Contains(detail.Body.String(), `"DeletedAt"`) || strings.Contains(detail.Body.String(), `"UserID"`) {
		t.Fatalf("notification detail serialized internal model fields: %s", detail.Body.String())
	}
	assertSensitiveResponseHeaders(t, detail)

	fullFormUpdate := request(http.MethodPut, "/notifications/1", map[string]any{
		"Name": "ops-renamed", "WebhookURL": secret, "Enabled": true, "Version": stored.Version,
	}, UpdateNotificationHandler)
	if fullFormUpdate.Code != http.StatusOK {
		t.Fatalf("full-form update status=%d body=%s", fullFormUpdate.Code, fullFormUpdate.Body.String())
	}
	if err := databaseNotificationDB().First(&stored, stored.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Name != "ops-renamed" || !stored.Enabled || stored.WebhookURL != secret || stored.Version != 2 {
		t.Fatalf("full-form update failed: %+v", stored)
	}

	omittedSecretUpdate := request(http.MethodPut, "/notifications/1", map[string]any{
		"Name": "ops-omitted-secret", "Version": stored.Version,
	}, UpdateNotificationHandler)
	if omittedSecretUpdate.Code != http.StatusOK {
		t.Fatalf("omitted-secret update status=%d body=%s", omittedSecretUpdate.Code, omittedSecretUpdate.Body.String())
	}
	if err := databaseNotificationDB().First(&stored, stored.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Name != "ops-omitted-secret" || stored.WebhookURL != secret || stored.Version != 3 {
		t.Fatalf("omitted secret was not retained: %+v", stored)
	}

	deleted := request(http.MethodDelete, "/notifications/1?version=3", nil, DeleteNotificationHandler)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
}

func TestNotificationDetailOwnership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	user, request := openNotificationTestDB(t)
	other := models.User{Username: "other", PasswordHash: "hash"}
	if err := databaseNotificationDB().Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	otherNotification := models.Notification{
		UserID: other.ID, Name: "other-target", Type: models.NotifyTypeWebhook, WebhookURL: "https://hooks.example/other", Version: 1,
	}
	if err := databaseNotificationDB().Create(&otherNotification).Error; err != nil {
		t.Fatal(err)
	}
	owned := models.Notification{
		UserID: user.ID, Name: "owned", Type: models.NotifyTypeWebhook, WebhookURL: "https://hooks.example/owned", Version: 1,
	}
	if err := databaseNotificationDB().Create(&owned).Error; err != nil {
		t.Fatal(err)
	}

	forbidden := request(http.MethodGet, "/notifications/1", nil, GetNotificationHandler)
	if forbidden.Code != http.StatusNotFound || strings.Contains(forbidden.Body.String(), otherNotification.WebhookURL) {
		t.Fatalf("cross-user detail status=%d body=%s", forbidden.Code, forbidden.Body.String())
	}
	assertSensitiveResponseHeaders(t, forbidden)

	detail := request(http.MethodGet, "/notifications/2", nil, GetNotificationHandler)
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), owned.WebhookURL) {
		t.Fatalf("owned detail status=%d body=%s", detail.Code, detail.Body.String())
	}
}

func TestNotificationExplicitEmptySecretIsRejectedWithoutChangingRecord(t *testing.T) {
	gin.SetMode(gin.TestMode)
	user, request := openNotificationTestDB(t)
	tests := []struct {
		name         string
		notification models.Notification
		payload      map[string]any
	}{
		{
			name: "webhook",
			notification: models.Notification{
				UserID: user.ID, Name: "webhook", Type: models.NotifyTypeWebhook, WebhookURL: "https://hooks.example/original", Version: 1,
			},
			payload: map[string]any{"Name": "changed-webhook", "WebhookURL": "", "Version": 1},
		},
		{
			name: "telegram",
			notification: models.Notification{
				UserID: user.ID, Name: "telegram", Type: models.NotifyTypeTelegram, TelegramToken: "original-token", TelegramChatID: "123", Version: 1,
			},
			payload: map[string]any{"Name": "changed-telegram", "TelegramToken": "", "Version": 1},
		},
	}
	for index := range tests {
		test := &tests[index]
		t.Run(test.name, func(t *testing.T) {
			if err := databaseNotificationDB().Create(&test.notification).Error; err != nil {
				t.Fatal(err)
			}
			updated := request(http.MethodPut, "/notifications/"+strconv.FormatUint(uint64(test.notification.ID), 10), test.payload, UpdateNotificationHandler)
			if updated.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", updated.Code, updated.Body.String())
			}
			var stored models.Notification
			if err := databaseNotificationDB().First(&stored, test.notification.ID).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Name != test.notification.Name || stored.WebhookURL != test.notification.WebhookURL ||
				stored.TelegramToken != test.notification.TelegramToken || stored.Version != test.notification.Version {
				t.Fatalf("invalid update changed stored notification: %+v", stored)
			}
		})
	}
}

func TestNotificationTestDraftUsesFormAndDoesNotWriteDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	user, request := openNotificationTestDB(t)
	var received bool
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer webhook.Close()

	response := request(http.MethodPost, "/notifications/test", map[string]any{
		"ID": 0, "Name": "draft", "Type": models.NotifyTypeWebhook, "WebhookURL": webhook.URL,
	}, TestNotificationHandler)
	if response.Code != http.StatusOK || !received {
		t.Fatalf("test status=%d received=%v body=%s", response.Code, received, response.Body.String())
	}
	var count int64
	if err := databaseNotificationDB().Model(&models.Notification{}).Where("user_id = ?", user.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("draft test wrote %d notification records", count)
	}
}

func TestNotificationSavedTestCanLoadStoredConfigurationWithoutWriting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	user, request := openNotificationTestDB(t)
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer webhook.Close()
	notification := models.Notification{UserID: user.ID, Name: "stored", Type: models.NotifyTypeWebhook, WebhookURL: webhook.URL, Enabled: true, Version: 1}
	if err := databaseNotificationDB().Create(&notification).Error; err != nil {
		t.Fatal(err)
	}

	response := request(http.MethodPost, "/notifications/test", map[string]any{"ID": notification.ID, "Version": notification.Version}, TestNotificationHandler)
	if response.Code != http.StatusOK {
		t.Fatalf("test status=%d body=%s", response.Code, response.Body.String())
	}
	var after models.Notification
	if err := databaseNotificationDB().First(&after, notification.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Name != notification.Name || after.WebhookURL != notification.WebhookURL || after.Version != notification.Version {
		t.Fatalf("saved test modified notification: %+v", after)
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
