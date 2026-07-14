package database

import (
	"cloudstream/internal/models"
	"cloudstream/internal/utils"
	"path/filepath"
	"testing"
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
