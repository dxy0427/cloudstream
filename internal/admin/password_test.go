package admin

import (
	"bytes"
	"cloudstream/internal/models"
	"cloudstream/internal/utils"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func createAdminTestDatabase(t *testing.T, users ...models.User) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "cloudstream.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatal(err)
	}
	for index := range users {
		if err := db.Create(&users[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	return dbPath
}

func readAdminTestUser(t *testing.T, dbPath string) models.User {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var user models.User
	if err := db.First(&user).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	return user
}

func TestRunSetsSpecifiedPasswordAndInvalidatesSessions(t *testing.T) {
	dbPath := createAdminTestDatabase(t, models.User{
		Username:              "renamed-admin",
		PasswordHash:          "old-hash",
		TokenVersion:          7,
		SiteTitle:             "Existing Site",
		NeedsPasswordReminder: true,
	})

	var output bytes.Buffer
	if err := Run([]string{"admin", "password", "NEW_PASSWORD"}, dbPath, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `管理员 "renamed-admin" 密码已重置`) {
		t.Fatalf("unexpected output: %q", output.String())
	}
	if strings.Contains(output.String(), "NEW_PASSWORD") {
		t.Fatalf("specified password was printed: %q", output.String())
	}

	user := readAdminTestUser(t, dbPath)
	if !utils.CheckPasswordHash(utils.SHA256Hex("NEW_PASSWORD"), user.PasswordHash) {
		t.Fatal("new password was not stored using the login hash format")
	}
	if user.TokenVersion != 8 {
		t.Fatalf("token version = %d, want 8", user.TokenVersion)
	}
	if user.NeedsPasswordReminder || !user.PasswordReminderShown {
		t.Fatalf("password reminder state was not updated: %+v", user)
	}
	if user.Username != "renamed-admin" || user.SiteTitle != "Existing Site" {
		t.Fatalf("unrelated user settings changed: %+v", user)
	}
}

func TestRunGeneratesPasswordAndDisplaysItOnce(t *testing.T) {
	dbPath := createAdminTestDatabase(t, models.User{
		Username:     "admin",
		PasswordHash: "old-hash",
		TokenVersion: 1,
	})

	var output bytes.Buffer
	if err := Run([]string{"admin", "password"}, dbPath, &output); err != nil {
		t.Fatal(err)
	}
	const marker = "新密码（仅显示一次）："
	if strings.Count(output.String(), marker) != 1 {
		t.Fatalf("generated password output = %q", output.String())
	}
	passwordLine := strings.SplitN(output.String(), marker, 2)[1]
	password := strings.TrimSpace(strings.SplitN(passwordLine, "\n", 2)[0])
	if len(password) != 32 {
		t.Fatalf("generated password length = %d, want 32", len(password))
	}

	user := readAdminTestUser(t, dbPath)
	if !utils.CheckPasswordHash(utils.SHA256Hex(password), user.PasswordHash) {
		t.Fatal("displayed generated password does not match the stored hash")
	}
	if user.TokenVersion != 2 {
		t.Fatalf("token version = %d, want 2", user.TokenVersion)
	}
}

func TestRunDoesNotResetRandomPasswordWhenOutputFails(t *testing.T) {
	dbPath := createAdminTestDatabase(t, models.User{
		Username:     "admin",
		PasswordHash: "old-hash",
		TokenVersion: 4,
	})

	if err := Run([]string{"admin", "password"}, dbPath, failingWriter{}); err == nil || !strings.Contains(err.Error(), "输出随机密码失败") {
		t.Fatalf("unexpected error: %v", err)
	}
	user := readAdminTestUser(t, dbPath)
	if user.PasswordHash != "old-hash" || user.TokenVersion != 4 {
		t.Fatalf("failed output changed credentials: %+v", user)
	}
}

func TestResetPasswordRejectsMissingDatabaseWithoutCreatingIt(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "missing", "cloudstream.db")
	if _, err := ResetPassword(dbPath, "new-password"); err == nil || !strings.Contains(err.Error(), "数据库不存在") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("missing database was created: %v", err)
	}
}

func TestResetPasswordRejectsEmptyOrAmbiguousUsers(t *testing.T) {
	emptyDB := createAdminTestDatabase(t)
	if _, err := ResetPassword(emptyDB, "new-password"); err == nil || !strings.Contains(err.Error(), "没有管理员") {
		t.Fatalf("empty database error: %v", err)
	}

	multipleDB := createAdminTestDatabase(t,
		models.User{Username: "first", PasswordHash: "first-hash"},
		models.User{Username: "second", PasswordHash: "second-hash"},
	)
	if _, err := ResetPassword(multipleDB, "new-password"); err == nil || !strings.Contains(err.Error(), "多个用户") {
		t.Fatalf("multiple users error: %v", err)
	}
}

func TestRunRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"admin"},
		{"admin", "unknown"},
		{"admin", "password", "one", "two"},
	} {
		if err := Run(args, "unused.db", &bytes.Buffer{}); err == nil || err.Error() != passwordCommandUsage {
			t.Fatalf("args=%v error=%v", args, err)
		}
	}
	if err := Run([]string{"admin", "password", ""}, "unused.db", &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "不能为空") {
		t.Fatalf("empty password error: %v", err)
	}
}
