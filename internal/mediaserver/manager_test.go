package mediaserver

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestReloadAllSkipsInvalidServer(t *testing.T) {
	previousDB := database.DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "media.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.MediaServer{}); err != nil {
		t.Fatal(err)
	}
	database.DB = db
	t.Cleanup(func() { database.DB = previousDB })

	valid := models.MediaServer{Name: "valid", ServerType: "Emby", ServerAddr: "https://example.com", APIKey: "key", Enabled: true}
	invalid := models.MediaServer{Name: "invalid", ServerType: "Emby", ServerAddr: "://bad", APIKey: "key", Enabled: true}
	if err := db.Create(&valid).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&invalid).Error; err != nil {
		t.Fatal(err)
	}

	manager := &Manager{servers: make(map[uint]*ProxyServer)}
	defer manager.CloseAll()
	if err := manager.ReloadAll(); err == nil {
		t.Fatal("expected invalid-server warning error")
	}
	if _, ok := manager.GetServer(valid.ID); !ok {
		t.Fatal("valid server was not loaded")
	}
	if _, ok := manager.GetServer(invalid.ID); ok {
		t.Fatal("invalid server was loaded")
	}
}

func TestValidateServerAddress(t *testing.T) {
	if err := ValidateServerAddress("https://example.com/emby"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateServerAddress("://bad"); err == nil {
		t.Fatal("expected invalid address")
	}
}

func TestProxyRetireWaitsForActiveRequest(t *testing.T) {
	proxy := NewProxyServer(&Config{Server: ServerConf{Addr: "https://example.com"}})
	if !proxy.beginRequest() {
		t.Fatal("failed to acquire initial request lease")
	}
	done := proxy.Retire()
	select {
	case <-done:
		t.Fatal("proxy retired before active request ended")
	case <-time.After(20 * time.Millisecond):
	}
	if proxy.beginRequest() {
		t.Fatal("retired proxy accepted a new request")
	}
	proxy.endRequest()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("proxy did not finish retiring")
	}
}
