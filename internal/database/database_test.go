package database

import (
	"cloudstream/internal/models"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOpenExistingDatabaseDoesNotCreateMissingFile(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "missing", "cloudstream.db")
	if _, err := OpenExistingDatabase(dbPath); err == nil || !strings.Contains(err.Error(), "数据库不存在") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("missing database was created: %v", err)
	}
}

func TestOpenExistingDatabaseUsesReadWriteMode(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "current.db")
	if err := os.WriteFile(dbPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := OpenExistingDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestConnectDatabaseRestrictsSQLiteFilePermissions(t *testing.T) {
	previousDB := DB
	dataDir := filepath.Join(t.TempDir(), "data")
	dbPath := filepath.Join(dataDir, "cloudstream.db")
	t.Setenv("CLOUDSTREAM_ADMIN_PASSWORD", "test-password")
	if err := ConnectDatabase(dbPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
		DB = previousDB
	})
	for _, path := range []string{dataDir, dbPath, dbPath + "-wal", dbPath + "-shm"} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		want := os.FileMode(0600)
		if info.IsDir() {
			want = 0700
		}
		if info.Mode().Perm() != want {
			t.Fatalf("path=%s permissions=%o want=%o", path, info.Mode().Perm(), want)
		}
	}
}

func TestAccountModelUsesCurrentPlaybackColumn(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "current.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Account{}); err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"playback_mode", "enable_stream_sign", "sign_expire_hours"} {
		if !db.Migrator().HasColumn("accounts", column) {
			t.Fatalf("current account model did not create column %q", column)
		}
	}
}

func TestPlaybackModeDefaultsByAccountType(t *testing.T) {
	tests := []struct {
		accountType string
		want        string
	}{
		{accountType: models.AccountType123Pan, want: models.PlaybackModeRedirect},
		{accountType: models.AccountTypeOpenList, want: models.PlaybackModeRedirect},
		{accountType: models.AccountTypeWebDAV, want: models.PlaybackModeProxy},
	}
	for _, tt := range tests {
		t.Run(tt.accountType, func(t *testing.T) {
			if got := models.DefaultPlaybackMode(tt.accountType); got != tt.want {
				t.Fatalf("default mode = %q, want %q", got, tt.want)
			}
			if got := models.NormalizePlaybackMode("", tt.accountType); got != tt.want {
				t.Fatalf("normalized mode = %q, want %q", got, tt.want)
			}
		})
	}
}
