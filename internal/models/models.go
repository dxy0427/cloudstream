package models

import "gorm.io/gorm"

// User：用户模型，存储登录凭证（替代旧的Setting模型）
type User struct {
	gorm.Model
	Username     string `gorm:"unique;not null"` // 唯一非空，登录用户名
	PasswordHash string `gorm:"not null"`        // 加密后的密码哈希（非明文存储）
	TokenVersion int    `gorm:"default:1"`       // Token版本号：修改用户名/密码后使旧Token失效
}

// Account：云账户模型，存储123网盘的独立配置
type Account struct {
	gorm.Model
	Name         string `gorm:"unique;not null" json:"Name"` // 唯一非空，账户别名（如“我的大号”）
	ClientID     string `json:"ClientID"`                    // 网盘API的Client ID
	ClientSecret string `json:"ClientSecret"`                // 网盘API的Client Secret
	StrmBaseURL  string `json:"StrmBaseURL"`                 // 生成STRM文件时的播放链接基础URL
}

// Task：任务模型，关联云账户的扫描同步任务配置
type Task struct {
	gorm.Model
	Name           string `gorm:"unique;not null" json:"Name"` // 唯一非空，任务名称
	AccountID      uint   `gorm:"not null" json:"AccountID"`   // 外键：关联Account表的ID（绑定执行任务的云账户）
	SourceFolderID string `gorm:"not null" json:"SourceFolderID"` // 云盘源目录ID（要扫描的目录）
	LocalPath      string `gorm:"not null" json:"LocalPath"`      // 本地存储路径（容器内路径，如“/app/strm/”）
	Cron           string `gorm:"not null" json:"Cron"`           // 定时执行表达式（UTC时区，如“0 */2 * * *”）
	Enabled        bool   `gorm:"default:true" json:"Enabled"`    // 任务是否启用（默认启用）
	Overwrite      bool   `gorm:"default:false" json:"Overwrite"`// 是否覆盖已存在的本地文件（默认不覆盖）
	StrmExtensions string `gorm:"default:'mp4,mkv,ts,iso'" json:"StrmExtensions"` // 生成STRM文件的后缀（默认视频格式）
	MetaExtensions string `gorm:"default:'jpg,jpeg,png,webp,srt,ass,sub'" json:"MetaExtensions"` // 下载元数据的后缀（默认封面、字幕等）
	Threads        int    `gorm:"default:4" json:"Threads"`      // 并发线程数（默认4，实际限制1-8）
}