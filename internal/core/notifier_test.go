package core

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNotificationErrorForLogRedactsURLs(t *testing.T) {
	secretURL := "https://hooks.example/secret-token"
	wrapped := &url.Error{Op: "Post", URL: secretURL, Err: errors.New("dial failed")}
	if logged := notificationErrorForLog(wrapped); strings.Contains(logged, "secret-token") || logged != "dial failed" {
		t.Fatalf("wrapped URL error was not redacted: %q", logged)
	}
	if logged := notificationErrorForLog(errors.New("request failed for " + secretURL)); strings.Contains(logged, "secret-token") {
		t.Fatalf("plain error URL was not redacted: %q", logged)
	}
}

func TestSendNotificationByEventTargetsEnabledMatches(t *testing.T) {
	previousDB := database.DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "notifier.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Notification{}); err != nil {
		t.Fatal(err)
	}
	database.DB = db
	t.Cleanup(func() { database.DB = previousDB })
	user := models.User{Username: "admin", PasswordHash: "hash"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}

	var matchingCalls atomic.Int32
	matching := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		matchingCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer matching.Close()
	var skippedCalls atomic.Int32
	skipped := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		skippedCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer skipped.Close()

	notifications := []models.Notification{
		{UserID: user.ID, Name: "matching", Type: models.NotifyTypeWebhook, WebhookURL: matching.URL, Enabled: true, NotifyOnError: true},
		{UserID: user.ID, Name: "wrong-event", Type: models.NotifyTypeWebhook, WebhookURL: skipped.URL, Enabled: true, NotifyOnComplete: true},
		{UserID: user.ID, Name: "disabled", Type: models.NotifyTypeWebhook, WebhookURL: skipped.URL, Enabled: false, NotifyOnError: true},
	}
	for index := range notifications {
		requested := notifications[index]
		if err := db.Create(&notifications[index]).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&notifications[index]).Updates(map[string]interface{}{
			"enabled": requested.Enabled, "notify_on_complete": requested.NotifyOnComplete,
			"notify_on_error": requested.NotifyOnError, "notify_on_stop": requested.NotifyOnStop,
			"notify_on_manual": requested.NotifyOnManual,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}

	SendNotificationByEvent("error", "message", NotifyEventError)
	if matchingCalls.Load() != 1 || skippedCalls.Load() != 0 {
		t.Fatalf("matching calls=%d skipped calls=%d", matchingCalls.Load(), skippedCalls.Load())
	}
}

func TestSendNotificationByEventIncludesAllUsers(t *testing.T) {
	previousDB := database.DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "multi-user-notifier.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Notification{}); err != nil {
		t.Fatal(err)
	}
	database.DB = db
	t.Cleanup(func() { database.DB = previousDB })

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	for index, username := range []string{"first", "second"} {
		user := models.User{Username: username, PasswordHash: "hash"}
		if err := db.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
		notification := models.Notification{UserID: user.ID, Name: username, Type: models.NotifyTypeWebhook, WebhookURL: server.URL, Enabled: true, NotifyOnComplete: true, Version: index + 1}
		if err := db.Create(&notification).Error; err != nil {
			t.Fatal(err)
		}
	}

	SendNotificationByEvent("complete", "message", NotifyEventComplete)
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}
