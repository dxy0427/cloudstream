package handlers

import (
	"cloudstream/internal/models"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func openMediaServerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openHandlerTestDB(t)
	if err := db.AutoMigrate(&models.MediaServer{}); err != nil {
		t.Fatal(err)
	}
	return db
}

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

func TestValidateMediaServerRejectsUnsafeLimitsAndNormalizesClients(t *testing.T) {
	server := models.MediaServer{
		Name: "emby", ServerType: "Emby", ServerAddr: "https://emby.example", APIKey: "key",
		HttpStrmTTL: 1441, ClientMode: "BlackList", ClientList: "[]", PathMappings: "[]",
	}
	if ok, _ := validateMediaServer(&server); ok {
		t.Fatal("oversized HTTPStrm TTL was accepted")
	}
	server.HttpStrmTTL = 1
	server.ClientList = `[" Emby ","emby"]`
	if ok, message := validateMediaServer(&server); !ok || message != "" {
		t.Fatalf("valid client list rejected: %s", message)
	}
	if server.ClientList != `["Emby"]` {
		t.Fatalf("normalized client list=%q", server.ClientList)
	}
	server.ClientList = `[""]`
	if ok, _ := validateMediaServer(&server); ok {
		t.Fatal("empty client keyword was accepted")
	}
	server.ClientList = "[]"
	server.PathMappings = `[{"old":"http://127.0.0.1:5244","new":"javascript:alert(1)"}]`
	if ok, _ := validateMediaServer(&server); ok {
		t.Fatal("unsafe path mapping was accepted")
	}
}

func TestMediaServerListRedactsAndDetailReturnsPlaintext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openMediaServerTestDB(t)
	server := models.MediaServer{
		Name: "emby", ServerType: "Emby", ServerAddr: "https://emby.example", APIKey: "saved-api-key",
		HttpStrmTTL: 1, ClientMode: "BlackList", ClientList: "[]", PathMappings: "[]", Port: 8091,
	}
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}

	listed := performHandlerRequest(t, http.MethodGet, "/mediaservers", nil, ListMediaServersHandler)
	if listed.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	if strings.Contains(listed.Body.String(), server.APIKey) || strings.Contains(listed.Body.String(), `"APIKey"`) || strings.Contains(listed.Body.String(), `"HasAPIKey"`) {
		t.Fatalf("media server list leaked API key data: %s", listed.Body.String())
	}
	var listResponse struct {
		Data []models.MediaServer `json:"data"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &listResponse); err != nil {
		t.Fatal(err)
	}
	if len(listResponse.Data) != 1 || listResponse.Data[0].Version != server.Version {
		t.Fatalf("media server list version = %+v, want %d", listResponse.Data, server.Version)
	}

	detail := performHandlerRequest(t, http.MethodGet, "/mediaservers/1", nil, GetMediaServerHandler)
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), server.APIKey) {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	if strings.Contains(detail.Body.String(), `"DeletedAt"`) {
		t.Fatalf("media server detail serialized the GORM model: %s", detail.Body.String())
	}
	var detailResponse struct {
		Data models.MediaServer `json:"data"`
	}
	if err := json.Unmarshal(detail.Body.Bytes(), &detailResponse); err != nil {
		t.Fatal(err)
	}
	if detailResponse.Data.Version != server.Version {
		t.Fatalf("media server detail version = %d, want %d", detailResponse.Data.Version, server.Version)
	}
	assertSensitiveResponseHeaders(t, detail)
}

func TestUpdateMediaServerRetainsOmittedAPIKeyAcrossBindingChange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openMediaServerTestDB(t)
	server := models.MediaServer{
		Name: "emby", ServerType: "Emby", ServerAddr: "https://old.example", APIKey: "saved-api-key",
		HttpStrmTTL: 1, ClientMode: "BlackList", ClientList: "[]", PathMappings: "[]", Enabled: true, Port: 8091,
	}
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&server).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}

	updated := performHandlerRequest(t, http.MethodPut, "/mediaservers/1", map[string]any{
		"ServerType": "Jellyfin",
		"ServerAddr": "https://new.example/",
		"Version":    server.Version,
	}, UpdateMediaServerHandler)
	if updated.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", updated.Code, updated.Body.String())
	}
	var stored models.MediaServer
	if err := db.First(&stored, server.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ServerType != "Jellyfin" || stored.ServerAddr != "https://new.example" || stored.APIKey != server.APIKey {
		t.Fatalf("unexpected stored server: %+v", stored)
	}
}

func TestUpdateMediaServerRejectsStaleCompleteForm(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openMediaServerTestDB(t)
	server := models.MediaServer{
		Name:             "emby",
		ServerType:       "Emby",
		ServerAddr:       "https://old.example",
		APIKey:           "old-api-key",
		CacheEnable:      true,
		HttpStrmTTL:      1,
		ClientEnable:     false,
		ClientMode:       "BlackList",
		ClientList:       "[]",
		HttpStrmEnable:   true,
		DisableTranscode: true,
		ResolveStrmLinks: true,
		UaPassthrough:    false,
		PathMappings:     "[]",
		Enabled:          true,
		Port:             8091,
	}
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}

	type detailResponse struct {
		Data models.MediaServer `json:"data"`
	}
	readView := func() detailResponse {
		t.Helper()
		response := performHandlerRequest(t, http.MethodGet, "/mediaservers/1", nil, GetMediaServerHandler)
		if response.Code != http.StatusOK {
			t.Fatalf("detail status=%d body=%s", response.Code, response.Body.String())
		}
		var view detailResponse
		if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}
		return view
	}
	firstView := readView()
	secondView := readView()
	if firstView.Data.Version != server.Version || secondView.Data.Version != server.Version {
		t.Fatalf("edit view versions=%d and %d, want %d", firstView.Data.Version, secondView.Data.Version, server.Version)
	}

	firstUpdate := performHandlerRequest(t, http.MethodPut, "/mediaservers/1", map[string]any{
		"Name":             "first-save",
		"ServerType":       "Jellyfin",
		"ServerAddr":       "https://new.example/",
		"APIKey":           "new-api-key",
		"CacheEnable":      false,
		"HttpStrmTTL":      2,
		"ClientEnable":     true,
		"ClientMode":       "WhiteList",
		"ClientList":       "[]",
		"HttpStrmEnable":   false,
		"DisableTranscode": false,
		"ResolveStrmLinks": false,
		"UaPassthrough":    true,
		"PathMappings":     "[]",
		"Enabled":          false,
		"Port":             8091,
		"Version":          firstView.Data.Version,
	}, UpdateMediaServerHandler)
	if firstUpdate.Code != http.StatusOK {
		t.Fatalf("first update status=%d body=%s", firstUpdate.Code, firstUpdate.Body.String())
	}
	var firstUpdateResponse detailResponse
	if err := json.Unmarshal(firstUpdate.Body.Bytes(), &firstUpdateResponse); err != nil {
		t.Fatal(err)
	}
	if firstUpdateResponse.Data.Version != server.Version+1 {
		t.Fatalf("first update version=%d, want %d", firstUpdateResponse.Data.Version, server.Version+1)
	}

	staleUpdate := performHandlerRequest(t, http.MethodPut, "/mediaservers/1", map[string]any{
		"Name":             secondView.Data.Name,
		"ServerType":       secondView.Data.ServerType,
		"ServerAddr":       secondView.Data.ServerAddr,
		"APIKey":           secondView.Data.APIKey,
		"CacheEnable":      secondView.Data.CacheEnable,
		"HttpStrmTTL":      secondView.Data.HttpStrmTTL,
		"ClientEnable":     secondView.Data.ClientEnable,
		"ClientMode":       secondView.Data.ClientMode,
		"ClientList":       secondView.Data.ClientList,
		"HttpStrmEnable":   secondView.Data.HttpStrmEnable,
		"DisableTranscode": secondView.Data.DisableTranscode,
		"ResolveStrmLinks": secondView.Data.ResolveStrmLinks,
		"UaPassthrough":    secondView.Data.UaPassthrough,
		"PathMappings":     secondView.Data.PathMappings,
		"Enabled":          secondView.Data.Enabled,
		"Port":             secondView.Data.Port,
		"Version":          secondView.Data.Version,
	}, UpdateMediaServerHandler)
	if staleUpdate.Code != http.StatusConflict {
		t.Fatalf("stale update status=%d body=%s", staleUpdate.Code, staleUpdate.Body.String())
	}

	var stored models.MediaServer
	if err := db.First(&stored, server.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Name != "first-save" || stored.ServerType != "Jellyfin" || stored.ServerAddr != "https://new.example" ||
		stored.APIKey != "new-api-key" || stored.CacheEnable || stored.HttpStrmTTL != 2 || !stored.ClientEnable ||
		stored.ClientMode != "WhiteList" || stored.HttpStrmEnable || stored.DisableTranscode || stored.ResolveStrmLinks ||
		!stored.UaPassthrough || stored.Enabled || stored.Version != server.Version+1 {
		t.Fatalf("stale form overwrote first update: %+v", stored)
	}
}

func TestUpdateMediaServerRejectsMissingVersionWithoutChangingRecord(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openMediaServerTestDB(t)
	server := models.MediaServer{
		Name: "emby", ServerType: "Emby", ServerAddr: "https://emby.example", APIKey: "saved-api-key",
		HttpStrmTTL: 1, ClientMode: "BlackList", ClientList: "[]", PathMappings: "[]", Port: 8091,
	}
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}

	response := performHandlerRequest(t, http.MethodPut, "/mediaservers/1", map[string]any{
		"Name": "renamed", "APIKey": server.APIKey,
	}, UpdateMediaServerHandler)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var stored models.MediaServer
	if err := db.First(&stored, server.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Name != server.Name || stored.APIKey != server.APIKey || stored.Version != server.Version {
		t.Fatalf("missing-version update changed stored server: %+v", stored)
	}
}

func TestUpdateMediaServerRejectsExplicitEmptyAPIKeyWithoutChangingRecord(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, value := range []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "whitespace", value: "   "},
	} {
		t.Run(value.name, func(t *testing.T) {
			db := openMediaServerTestDB(t)
			server := models.MediaServer{
				Name: "emby", ServerType: "Emby", ServerAddr: "https://emby.example", APIKey: "saved-api-key",
				HttpStrmTTL: 1, ClientMode: "BlackList", ClientList: "[]", PathMappings: "[]", Port: 8091,
			}
			if err := db.Create(&server).Error; err != nil {
				t.Fatal(err)
			}

			updated := performHandlerRequest(t, http.MethodPut, "/mediaservers/1", map[string]any{
				"Name": "should-not-save", "APIKey": value.value, "Version": server.Version,
			}, UpdateMediaServerHandler)
			if updated.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", updated.Code, updated.Body.String())
			}
			var stored models.MediaServer
			if err := db.First(&stored, server.ID).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Name != server.Name || stored.APIKey != server.APIKey || stored.Version != server.Version {
				t.Fatalf("invalid update changed stored server: %+v", stored)
			}
		})
	}
}

func TestMediaServerConnectionUsesDraftAndDoesNotWriteDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openMediaServerTestDB(t)
	storedServer := models.MediaServer{
		Name: "stored", ServerType: "Emby", ServerAddr: "https://stored.example", APIKey: "stored-key",
		HttpStrmTTL: 1, ClientMode: "BlackList", ClientList: "[]", PathMappings: "[]", Port: 8091,
	}
	if err := db.Create(&storedServer).Error; err != nil {
		t.Fatal(err)
	}

	missingKey := performHandlerRequest(t, http.MethodPost, "/mediaservers/test", map[string]any{
		"ID": storedServer.ID, "Name": "draft", "ServerType": "Emby", "ServerAddr": "https://draft.example",
	}, TestMediaServerConnectionHandler)
	if missingKey.Code != http.StatusBadRequest {
		t.Fatalf("missing-key status=%d body=%s", missingKey.Code, missingKey.Body.String())
	}

	var receivedKey string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKey = r.Header.Get("X-Emby-Token")
		if r.URL.Path != "/System/Info" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	tested := performHandlerRequest(t, http.MethodPost, "/mediaservers/test", map[string]any{
		"ID": storedServer.ID, "Name": "draft", "ServerType": "Emby", "ServerAddr": upstream.URL,
		"APIKey": "draft-key", "HttpStrmTTL": 1, "ClientMode": "BlackList", "ClientList": "[]", "PathMappings": "[]",
	}, TestMediaServerConnectionHandler)
	if tested.Code != http.StatusOK || receivedKey != "draft-key" {
		t.Fatalf("status=%d key=%q body=%s", tested.Code, receivedKey, tested.Body.String())
	}

	var after models.MediaServer
	if err := db.First(&after, storedServer.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Name != storedServer.Name || after.ServerAddr != storedServer.ServerAddr || after.APIKey != storedServer.APIKey {
		t.Fatalf("connection test modified stored server: %+v", after)
	}
}
