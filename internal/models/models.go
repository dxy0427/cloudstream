package models

import (
	"gorm.io/gorm"
)

const (
	AccountType123Pan   = "123pan"
	AccountTypeOpenList = "openlist"

	NotifyTypeWebhook  = "webhook"
	NotifyTypeTelegram = "telegram"
)

type User struct {
	gorm.Model
	Username     string `gorm:"unique;not null"`
	PasswordHash string `gorm:"not null"`
	TokenVersion int    `gorm:"default:1"`

	NotifyType     string `gorm:"default:'webhook'" json:"NotifyType"`
	WebhookURL     string `json:"WebhookURL"`
	TelegramToken  string `json:"TelegramToken"`
	TelegramChatID string `json:"TelegramChatID"`

	NotifyOnComplete bool `gorm:"default:true" json:"NotifyOnComplete"`
	NotifyOnError    bool `gorm:"default:true" json:"NotifyOnError"`
	NotifyOnStop     bool `gorm:"default:true" json:"NotifyOnStop"`

	SiteTitle string `gorm:"default:'CloudStream'" json:"SiteTitle"`
	Theme     string `gorm:"default:'light'" json:"Theme"`

	NeedsPasswordReminder bool `gorm:"default:false" json:"NeedsPasswordReminder"`
	PasswordReminderShown bool `gorm:"default:false" json:"PasswordReminderShown"`
}

type Account struct {
	gorm.Model
	Name         string `gorm:"unique;not null" json:"Name"`
	Type         string `gorm:"not null;default:'123pan'" json:"Type"`
	ClientID     string `json:"ClientID"`
	ClientSecret string `json:"ClientSecret"`

	OpenListURL      string `json:"OpenListURL"`
	OpenListToken    string `json:"OpenListToken"`
	OpenListUsername string `json:"OpenListUsername"`
	OpenListPassword string `json:"OpenListPassword"`

	StrmBaseURL string `json:"StrmBaseURL"`
	CacheTTL    int    `gorm:"default:1" json:"CacheTTL"`
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
	EncodePath     bool   `gorm:"default:false" json:"EncodePath"`
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

type TaskRunHistory struct {
	gorm.Model
	TaskID          uint   `gorm:"index;not null" json:"TaskID"`
	TaskName        string `gorm:"not null" json:"TaskName"`
	RunMode         string `gorm:"not null" json:"RunMode"`
	Status          string `gorm:"not null" json:"Status"`
	NewStrmCount    int    `gorm:"default:0" json:"NewStrmCount"`
	NewMetaCount    int    `gorm:"default:0" json:"NewMetaCount"`
	DeletedCount    int    `gorm:"default:0" json:"DeletedCount"`
	TotalStrmCount  int    `gorm:"default:0" json:"TotalStrmCount"`
	TotalMetaCount  int    `gorm:"default:0" json:"TotalMetaCount"`
	ProcessedCount  int    `gorm:"default:0" json:"ProcessedCount"`
	NotificationSent bool  `gorm:"default:false" json:"NotificationSent"`
	Message         string `gorm:"type:text" json:"Message"`
}

type MediaServer struct {
	gorm.Model
	Name string `gorm:"unique;not null" json:"Name"`

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
