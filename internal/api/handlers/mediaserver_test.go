package handlers

import (
	"cloudstream/internal/models"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestMediaServerCreateBooleanDefaults(t *testing.T) {
	if !boolOrDefault(nil, true) {
		t.Fatal("omitted true-default boolean became false")
	}
	if boolOrDefault(nil, false) {
		t.Fatal("omitted false-default boolean became true")
	}
	value := false
	if boolOrDefault(&value, true) {
		t.Fatal("explicit false was not preserved")
	}
}

func TestRevealMediaServerAPIKeyIsNoStoreAndBindingChecked(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	if err := db.AutoMigrate(&models.MediaServer{}); err != nil {
		t.Fatal(err)
	}
	server := models.MediaServer{Name: "emby", ServerType: "Emby", ServerAddr: "https://emby.example", APIKey: "saved-api-key"}
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}

	revealed := performHandlerRequest(t, http.MethodPost, "/mediaservers/1", map[string]any{
		"field": "api_key", "serverType": "Emby", "serverAddr": "https://emby.example/", "updatedAt": server.UpdatedAt.Format(time.RFC3339Nano),
	}, RevealMediaServerSecretHandler)
	if revealed.Code != http.StatusOK || !strings.Contains(revealed.Body.String(), "saved-api-key") {
		t.Fatalf("status=%d body=%s", revealed.Code, revealed.Body.String())
	}
	if !strings.Contains(revealed.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("Cache-Control=%q", revealed.Header().Get("Cache-Control"))
	}

	changed := performHandlerRequest(t, http.MethodPost, "/mediaservers/1", map[string]any{
		"field": "api_key", "serverType": "Jellyfin", "serverAddr": "https://emby.example", "updatedAt": server.UpdatedAt.Format(time.RFC3339Nano),
	}, RevealMediaServerSecretHandler)
	if changed.Code != http.StatusConflict {
		t.Fatalf("changed binding status=%d body=%s", changed.Code, changed.Body.String())
	}
	stale := performHandlerRequest(t, http.MethodPost, "/mediaservers/1", map[string]any{
		"field": "api_key", "serverType": "Emby", "serverAddr": "https://emby.example", "updatedAt": time.Now().Add(-time.Hour).Format(time.RFC3339Nano),
	}, RevealMediaServerSecretHandler)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale status=%d body=%s", stale.Code, stale.Body.String())
	}
	omittedBinding := performHandlerRequest(t, http.MethodPost, "/mediaservers/1", map[string]any{
		"field": "api_key", "updatedAt": server.UpdatedAt.Format(time.RFC3339Nano),
	}, RevealMediaServerSecretHandler)
	if omittedBinding.Code != http.StatusBadRequest {
		t.Fatalf("omitted binding status=%d body=%s", omittedBinding.Code, omittedBinding.Body.String())
	}
}
