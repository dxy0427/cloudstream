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
}

type Account struct {
	gorm.Model
	Name             string `gorm:"unique;not null" json:"Name"`
	Type             string `gorm:"not null;default:'123pan'" json:"Type"`
	ClientID         string `json:"ClientID"`
	ClientSecret     string `json:"ClientSecret"`
	
	// OpenList 配置
	OpenListURL      string `json:"OpenListURL"`
	OpenListToken    string `json:"OpenListToken"`    // 静态 Token
	OpenListUsername string `json:"OpenListUsername"` // 动态登录用户名
	OpenListPassword string `json:"OpenListPassword"` // 动态登录密码

	// 通用配置
	StrmBaseURL string `json:"StrmBaseURL"`
	CacheTTL    int    `gorm:"default:1" json:"CacheTTL"` // 目录缓存时间(分钟)，0为不缓存
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

// 媒体服务器配置
type MediaServer struct {
	gorm.Model
	Name string `gorm:"unique;not null" json:"Name"`
	
	// 服务配置
	ServerType string `gorm:"not null;default:'Emby'" json:"ServerType"` // Emby 或 Jellyfin
	ServerAddr string `gorm:"not null" json:"ServerAddr"`
	APIKey     string `gorm:"not null" json:"APIKey"`
	
	// 缓存配置
	CacheEnable     bool `gorm:"default:true" json:"CacheEnable"`
	HttpStrmTTL     int  `gorm:"default:1" json:"HttpStrmTTL"` // 分钟
	
	// 客户端过滤
	ClientEnable bool     `gorm:"default:false" json:"ClientEnable"`
	ClientMode   string   `gorm:"default:'BlackList'" json:"ClientMode"` // WhiteList 或 BlackList
	ClientList   string   `gorm:"type:text" json:"ClientList"` // JSON数组字符串
	
	// HTTPStrm配置
	HttpStrmEnable           bool   `gorm:"default:true" json:"HttpStrmEnable"`
	DisableTranscode         bool   `gorm:"default:true" json:"DisableTranscode"`
	ResolveStrmLinks         bool   `gorm:"default:true" json:"ResolveStrmLinks"`
	UaPassthrough            bool   `gorm:"default:false" json:"UaPassthrough"`
	PathMappings             string `gorm:"type:text" json:"PathMappings"` // JSON数组字符串
	
	// 运行状态
	Enabled    bool `gorm:"default:true" json:"Enabled"`
	Port       int  `gorm:"default:8091" json:"Port"`
}