package database

import (
	"cloudstream/internal/models"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

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
