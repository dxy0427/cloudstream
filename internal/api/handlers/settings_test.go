package handlers

import (
	"bytes"
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/utils"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestUpdateCredentialsValidatesHashesAndClearsSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatal(err)
	}
	current := utils.SHA256Hex("current-password")
	hash, err := utils.HashPassword(current)
	if err != nil {
		t.Fatal(err)
	}
	user := models.User{Username: "admin", PasswordHash: hash, TokenVersion: 1}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}

	perform := func(payload map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/update_credentials", &body)
		ctx.Request.Header.Set("Content-Type", "application/json")
		ctx.Set("username", "admin")
		UpdateCredentialsHandler(ctx)
		return recorder
	}

	invalid := perform(map[string]any{"currentPassword": "plaintext", "newUsername": "renamed"})
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "格式无效") {
		t.Fatalf("invalid status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	updated := perform(map[string]any{"currentPassword": current, "newUsername": "renamed"})
	if updated.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	if !strings.Contains(updated.Header().Get("Set-Cookie"), "cloudstream_token=;") || !strings.Contains(updated.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("session cookie was not cleared: %q", updated.Header().Get("Set-Cookie"))
	}
	var stored models.User
	if err := database.DB.First(&stored, user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Username != "renamed" || stored.TokenVersion != 2 {
		t.Fatalf("stored user=%+v", stored)
	}
}
