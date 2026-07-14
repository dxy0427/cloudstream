package database

import (
	"cloudstream/internal/models"
	"cloudstream/internal/utils"
	"fmt"
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

func TestWebDAVPlaybackModeMigrationFromLegacyBoolean(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy-playback.db")
	createLegacyAccountsTable(t, dbPath, true, false)
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO accounts (name, type, web_dav_direct_link) VALUES
		('proxy-account', 'webdav', 0),
		('redirect-account', 'webdav', 1)`).Error; err != nil {
		t.Fatal(err)
	}
	closeGormDB(t, db)

	connectMigrationTestDatabase(t, dbPath)
	var accounts []models.Account
	if err := DB.Order("id asc").Find(&accounts).Error; err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 {
		t.Fatalf("account count = %d, want 2", len(accounts))
	}
	if accounts[0].WebDAVPlaybackMode != models.WebDAVPlaybackModeProxy || accounts[0].WebDAVDirectLink {
		t.Fatalf("legacy false migrated incorrectly: %+v", accounts[0])
	}
	if accounts[1].WebDAVPlaybackMode != models.WebDAVPlaybackModeUpstreamRedirect || !accounts[1].WebDAVDirectLink {
		t.Fatalf("legacy true migrated incorrectly: %+v", accounts[1])
	}
}

func TestWebDAVPlaybackModeMigrationWithoutLegacyBoolean(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "older-playback.db")
	createLegacyAccountsTable(t, dbPath, false, false)
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO accounts (name, type) VALUES ('older-account', 'webdav')`).Error; err != nil {
		t.Fatal(err)
	}
	closeGormDB(t, db)

	connectMigrationTestDatabase(t, dbPath)
	var account models.Account
	if err := DB.Where("name = ?", "older-account").First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.WebDAVPlaybackMode != models.WebDAVPlaybackModeProxy || account.WebDAVDirectLink {
		t.Fatalf("older account migrated incorrectly: %+v", account)
	}
}

func TestWebDAVPlaybackModeMigrationRepairsOnlyInvalidValuesAndIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "existing-playback.db")
	createLegacyAccountsTable(t, dbPath, true, true)
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO accounts (name, type, web_dav_direct_link, web_dav_playback_mode) VALUES
		('valid-proxy', 'webdav', 1, 'proxy'),
		('valid-redirect', 'webdav', 0, 'upstream-redirect'),
		('empty-mode', 'webdav', 1, ''),
		('unknown-mode', 'webdav', 1, 'future-mode')`).Error; err != nil {
		t.Fatal(err)
	}
	closeGormDB(t, db)

	connectMigrationTestDatabase(t, dbPath)
	if err := migrateWebDAVPlaybackMode(); err != nil {
		t.Fatalf("second migration failed: %v", err)
	}
	var accounts []models.Account
	if err := DB.Order("id asc").Find(&accounts).Error; err != nil {
		t.Fatal(err)
	}
	want := []string{
		models.WebDAVPlaybackModeProxy,
		models.WebDAVPlaybackModeUpstreamRedirect,
		models.WebDAVPlaybackModeProxy,
		models.WebDAVPlaybackModeProxy,
	}
	for i, account := range accounts {
		if account.WebDAVPlaybackMode != want[i] {
			t.Errorf("account %q mode = %q, want %q", account.Name, account.WebDAVPlaybackMode, want[i])
		}
	}
	if !accounts[0].WebDAVDirectLink || accounts[1].WebDAVDirectLink {
		t.Fatalf("migration overwrote compatibility booleans for existing legal modes: %+v", accounts)
	}
}

func TestNewAccountDefaultsToProxyPlaybackMode(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "new-playback.db")
	connectMigrationTestDatabase(t, dbPath)
	account := models.Account{Name: "new-webdav", Type: models.AccountTypeWebDAV, WebDAVURL: "https://dav.example"}
	if err := DB.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := DB.First(&account, account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if account.WebDAVPlaybackMode != models.WebDAVPlaybackModeProxy {
		t.Fatalf("mode = %q, want proxy", account.WebDAVPlaybackMode)
	}
}

func createLegacyAccountsTable(t *testing.T, dbPath string, withDirectLink, withPlaybackMode bool) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	columns := ""
	if withDirectLink {
		columns += ", web_dav_direct_link numeric DEFAULT 0"
	}
	if withPlaybackMode {
		columns += ", web_dav_playback_mode text"
	}
	statement := fmt.Sprintf(`CREATE TABLE accounts (
		id integer PRIMARY KEY AUTOINCREMENT,
		created_at datetime,
		updated_at datetime,
		deleted_at datetime,
		name text NOT NULL UNIQUE,
		type text NOT NULL DEFAULT 'webdav'%s
	)`, columns)
	if err := db.Exec(statement).Error; err != nil {
		t.Fatal(err)
	}
	closeGormDB(t, db)
}

func connectMigrationTestDatabase(t *testing.T, dbPath string) {
	t.Helper()
	previousDB := DB
	t.Setenv("CLOUDSTREAM_ADMIN_PASSWORD", "migration-test-password")
	if err := ConnectDatabase(dbPath); err != nil {
		t.Fatal(err)
	}
	connectedDB := DB
	t.Cleanup(func() {
		closeGormDB(t, connectedDB)
		DB = previousDB
	})
}

func closeGormDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
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
