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
	
	// 通知配置
	NotifyType      string `gorm:"default:'webhook'" json:"NotifyType"` // webhook 或 telegram
	WebhookURL      string `json:"WebhookURL"`
	TelegramToken   string `json:"TelegramToken"`
	TelegramChatID  string `json:"TelegramChatID"`
}

type Account struct {
	gorm.Model
	Name          string `gorm:"unique;not null" json:"Name"`
	Type          string `gorm:"not null;default:'123pan'" json:"Type"`
	ClientID      string `json:"ClientID"`
	ClientSecret  string `json:"ClientSecret"`
	OpenListURL   string `json:"OpenListURL"`
	OpenListToken string `json:"OpenListToken"`
	StrmBaseURL   string `json:"StrmBaseURL"`
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
}

type TaskFile struct {
	ID        uint   `gorm:"primarykey"`
	TaskID    uint   `gorm:"index;uniqueIndex:idx_task_file;not null"` 
	FilePath  string `gorm:"index;uniqueIndex:idx_task_file;not null"` 
}