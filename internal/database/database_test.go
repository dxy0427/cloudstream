package database

import (
	"cloudstream/internal/models"
	"cloudstream/internal/utils"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestResetUserPasswordWorksAfterUsernameChange(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cloudstream.db")
	previousDB := DB
	t.Setenv("CLOUDSTREAM_ADMIN_PASSWORD", "initial-password")
	if err := ConnectDatabase(dbPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
		DB = previousDB
	})

	var user models.User
	if err := DB.First(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := DB.Model(&user).Update("username", "renamed-admin").Error; err != nil {
		t.Fatal(err)
	}
	if err := resetUserPassword(user.ID, "replacement-password"); err != nil {
		t.Fatal(err)
	}
	if err := DB.First(&user, user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !utils.CheckPasswordHash(utils.SHA256Hex("replacement-password"), user.PasswordHash) {
		t.Fatal("reset password hash did not match")
	}
}

func TestAccountDirectLinkColumnMigratesWithFalseDefault(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE accounts (
		id integer PRIMARY KEY AUTOINCREMENT,
		created_at datetime,
		updated_at datetime,
		deleted_at datetime,
		name text NOT NULL UNIQUE,
		type text NOT NULL DEFAULT 'webdav',
		web_dav_url text,
		web_dav_username text,
		web_dav_password text
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO accounts (name, type, web_dav_url) VALUES ('legacy', 'webdav', 'https://files.example/dav')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Account{}); err != nil {
		t.Fatal(err)
	}
	var account models.Account
	if err := db.Where("name = ?", "legacy").First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.WebDAVDirectLink {
		t.Fatal("legacy WebDAV account unexpectedly enabled direct-link mode")
	}
}

func TestLegacyNotificationMigration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "notifications.db")
	previousDB := DB
	t.Setenv("CLOUDSTREAM_ADMIN_PASSWORD", "initial-password")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatal(err)
	}
	legacy := models.User{
		Username: "admin", PasswordHash: "hash", NotifyType: models.NotifyTypeWebhook,
		WebhookURL: "https://hooks.example/legacy", TelegramToken: "telegram-token", TelegramChatID: "123",
		NotifyOnComplete: true, NotifyOnError: false, NotifyOnStop: true, NotifyOnManual: false,
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&legacy).Updates(map[string]interface{}{
		"notify_on_complete": true,
		"notify_on_error":    false,
		"notify_on_stop":     true,
		"notify_on_manual":   false,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}

	if err := ConnectDatabase(dbPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
		DB = previousDB
	})

	var notifications []models.Notification
	if err := DB.Where("user_id = ?", legacy.ID).Order("id asc").Find(&notifications).Error; err != nil {
		t.Fatal(err)
	}
	if len(notifications) != 2 {
		t.Fatalf("notification count = %d, want 2", len(notifications))
	}
	if notifications[0].Type != models.NotifyTypeWebhook || !notifications[0].Enabled || notifications[0].NotifyOnError || notifications[0].NotifyOnManual {
		t.Fatalf("unexpected migrated webhook: %+v", notifications[0])
	}
	if notifications[1].Type != models.NotifyTypeTelegram || notifications[1].Enabled {
		t.Fatalf("unexpected migrated telegram: %+v", notifications[1])
	}
	var migratedUser models.User
	if err := DB.First(&migratedUser, legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if migratedUser.WebhookURL != legacy.WebhookURL || migratedUser.TelegramToken != legacy.TelegramToken || migratedUser.TelegramChatID != legacy.TelegramChatID {
		t.Fatalf("legacy settings were not retained for rollback: %+v", migratedUser)
	}
	if migratedUser.NotificationMigrationFingerprint == "" {
		t.Fatal("migration fingerprint was not saved")
	}

	if err := migrateLegacyNotifications(); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := DB.Model(&models.Notification{}).Where("user_id = ?", legacy.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("migration was not idempotent, count = %d", count)
	}

	if err := DB.Model(&models.User{}).Where("id = ?", legacy.ID).Updates(map[string]interface{}{
		"webhook_url":        "https://hooks.example/rollback-change",
		"notify_on_complete": false,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacyNotifications(); err != nil {
		t.Fatal(err)
	}
	var webhook models.Notification
	if err := DB.Where("user_id = ? AND legacy_source = ?", legacy.ID, models.NotifyTypeWebhook).First(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	if webhook.WebhookURL != "https://hooks.example/rollback-change" || webhook.NotifyOnComplete {
		t.Fatalf("rollback changes were not synchronized: %+v", webhook)
	}
}

func TestLegacyNotificationMigrationPreservesPartialTelegram(t *testing.T) {
	previousDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "partial-notification.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Notification{}); err != nil {
		t.Fatal(err)
	}
	user := models.User{Username: "admin", PasswordHash: "hash", NotifyType: models.NotifyTypeTelegram, TelegramToken: "partial-token"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	DB = db
	t.Cleanup(func() { DB = previousDB })
	if err := migrateLegacyNotifications(); err != nil {
		t.Fatal(err)
	}
	var notification models.Notification
	if err := DB.Where("user_id = ? AND legacy_source = ?", user.ID, models.NotifyTypeTelegram).First(&notification).Error; err != nil {
		t.Fatal(err)
	}
	if notification.TelegramToken != "partial-token" || notification.TelegramChatID != "" || notification.Enabled {
		t.Fatalf("partial Telegram credentials were not preserved safely: %+v", notification)
	}
}
