package models

import (
	"gorm.io/gorm"
)

const (
	AccountType123Pan   = "123pan"
	AccountTypeOpenList = "openlist"
	AccountTypeWebDAV   = "webdav"

	PlaybackModeProxy    = "proxy"
	PlaybackModeRedirect = "redirect"

	NotifyTypeWebhook  = "webhook"
	NotifyTypeTelegram = "telegram"
)

func IsValidPlaybackMode(mode string) bool {
	return mode == PlaybackModeProxy || mode == PlaybackModeRedirect
}

func DefaultPlaybackMode(accountType string) string {
	switch accountType {
	case AccountType123Pan, AccountTypeOpenList:
		return PlaybackModeRedirect
	default:
		return PlaybackModeProxy
	}
}

func NormalizePlaybackMode(mode, accountType string) string {
	if !IsValidPlaybackMode(mode) {
		return DefaultPlaybackMode(accountType)
	}
	return mode
}

type User struct {
	gorm.Model
	Username     string `gorm:"unique;not null"`
	PasswordHash string `gorm:"not null"`
	TokenVersion int    `gorm:"default:1"`

	SiteTitle string `gorm:"default:'CloudStream'" json:"SiteTitle"`
	Theme     string `gorm:"default:'light'" json:"Theme"`

	NeedsPasswordReminder bool `gorm:"default:false" json:"NeedsPasswordReminder"`
	PasswordReminderShown bool `gorm:"default:false" json:"PasswordReminderShown"`
}

type Notification struct {
	gorm.Model
	UserID  uint   `gorm:"not null;index;uniqueIndex:idx_notification_user_name" json:"UserID"`
	Name    string `gorm:"not null;uniqueIndex:idx_notification_user_name" json:"Name"`
	Type    string `gorm:"not null" json:"Type"`
	Version int    `gorm:"not null;default:1" json:"Version"`

	WebhookURL     string `json:"WebhookURL"`
	TelegramToken  string `json:"TelegramToken"`
	TelegramChatID string `json:"TelegramChatID"`

	Enabled          bool `gorm:"default:true" json:"Enabled"`
	NotifyOnComplete bool `gorm:"default:true" json:"NotifyOnComplete"`
	NotifyOnError    bool `gorm:"default:true" json:"NotifyOnError"`
	NotifyOnStop     bool `gorm:"default:true" json:"NotifyOnStop"`
	NotifyOnManual   bool `gorm:"default:true" json:"NotifyOnManual"`
}

type Account struct {
	gorm.Model
	Version      int    `gorm:"not null;default:1" json:"Version"`
	Name         string `gorm:"unique;not null" json:"Name"`
	Type         string `gorm:"not null;default:'123pan'" json:"Type"`
	ClientID     string `json:"ClientID"`
	ClientSecret string `json:"ClientSecret"`

	OpenListURL      string `json:"OpenListURL"`
	OpenListAuthMode string `json:"OpenListAuthMode"`
	OpenListToken    string `json:"OpenListToken"`
	OpenListUsername string `json:"OpenListUsername"`
	OpenListPassword string `json:"OpenListPassword"`

	WebDAVURL      string `json:"WebDAVURL"`
	WebDAVUsername string `json:"WebDAVUsername"`
	WebDAVPassword string `json:"WebDAVPassword"`

	StrmBaseURL         string `json:"StrmBaseURL"`
	PlaybackMode        string `gorm:"not null" json:"PlaybackMode"`
	EnableStreamSign    bool   `gorm:"default:false" json:"EnableStreamSign"`
	SignExpireHours     int    `gorm:"default:0" json:"SignExpireHours"`
	CacheTTL            int    `gorm:"default:30" json:"CacheTTL"`
	CustomCachePolicies string `gorm:"type:text" json:"CustomCachePolicies"`
}

type Task struct {
	gorm.Model
	Name           string `gorm:"unique;not null" json:"Name"`
	AccountID      uint   `gorm:"not null" json:"AccountID"`
	SourceFolderID string `gorm:"not null" json:"SourceFolderID"`
	LocalPath      string `gorm:"not null" json:"LocalPath"`
	Cron           string `gorm:"not null" json:"Cron"`
	Enabled        bool   `gorm:"default:true" json:"Enabled"`
	Overwrite      bool   `gorm:"default:false" json:"Overwrite"`
	SyncDelete     bool   `gorm:"default:false" json:"SyncDelete"`
	StrmExtensions string `gorm:"default:'mp4,mkv,ts,iso'" json:"StrmExtensions"`
	MetaExtensions string `gorm:"default:'jpg,jpeg,png,webp,srt,ass,sub'" json:"MetaExtensions"`
	Threads        int    `gorm:"default:4" json:"Threads"`

	ProcessedCount int    `gorm:"default:0" json:"ProcessedCount"`
	LastRunStatus  string `gorm:"default:''" json:"LastRunStatus"`
}

type TaskFile struct {
	ID       uint   `gorm:"primarykey"`
	TaskID   uint   `gorm:"index;uniqueIndex:idx_task_file;not null"`
	FilePath string `gorm:"index;uniqueIndex:idx_task_file;not null"`
}

type MediaServer struct {
	gorm.Model
	Version int    `gorm:"not null;default:1" json:"Version"`
	Name    string `gorm:"unique;not null" json:"Name"`

	ServerType string `gorm:"not null;default:'Emby'" json:"ServerType"`
	ServerAddr string `gorm:"not null" json:"ServerAddr"`
	APIKey     string `gorm:"not null" json:"APIKey"`

	CacheEnable bool `gorm:"default:true" json:"CacheEnable"`
	HttpStrmTTL int  `gorm:"default:1" json:"HttpStrmTTL"`

	ClientEnable bool   `gorm:"default:false" json:"ClientEnable"`
	ClientMode   string `gorm:"default:'BlackList'" json:"ClientMode"`
	ClientList   string `gorm:"type:text" json:"ClientList"`

	HttpStrmEnable   bool   `gorm:"default:true" json:"HttpStrmEnable"`
	DisableTranscode bool   `gorm:"default:true" json:"DisableTranscode"`
	ResolveStrmLinks bool   `gorm:"default:true" json:"ResolveStrmLinks"`
	UaPassthrough    bool   `gorm:"default:false" json:"UaPassthrough"`
	PathMappings     string `gorm:"type:text" json:"PathMappings"`

	Enabled bool `gorm:"default:true" json:"Enabled"`
	Port    int  `gorm:"default:8091" json:"Port"`
}
