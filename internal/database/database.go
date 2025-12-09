package database

import (
	"cloudstream/internal/models"
	"cloudstream/internal/utils"
	"fmt"
	"github.com/rs/zerolog/log"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"path/filepath"
)

var DB *gorm.DB

func ConnectDatabase(dbPath string) error {
	var err error

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}

	// 开启 WAL 模式的关键配置
	dbConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	}

	DB, err = gorm.Open(sqlite.Open(dbPath), dbConfig)
	if err != nil {
		return fmt.Errorf("连接数据库失败: %w", err)
	}

	// 获取底层 SQL DB 对象进行配置
	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}

	// 核心优化：开启 WAL 模式，大幅提升并发性能
	// WAL (Write-Ahead Logging) 允许同时进行读写操作
	if _, err := sqlDB.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		log.Warn().Err(err).Msg("开启 SQLite WAL 模式失败，性能可能受限")
	}
	// 稍微调高繁忙超时时间
	if _, err := sqlDB.Exec("PRAGMA busy_timeout=5000;"); err != nil {
		log.Warn().Err(err).Msg("设置 busy_timeout 失败")
	}

	// 自动迁移结构
	err = DB.AutoMigrate(
		&models.User{},
		&models.Task{},
		&models.Account{},
		&models.TaskFile{},
	)
	if err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}

	var userCount int64
	if err := DB.Model(&models.User{}).Count(&userCount).Error; err != nil {
		return fmt.Errorf("统计用户数量失败: %w", err)
	}

	if userCount == 0 {
		log.Info().Msg("未发现用户，正在创建默认管理员 admin/admin...")
		hashedPassword, _ := utils.HashPassword("admin")
		defaultUser := models.User{
			Username:     "admin",
			PasswordHash: hashedPassword,
			TokenVersion: 1,
		}
		if err := DB.Create(&defaultUser).Error; err != nil {
			return fmt.Errorf("创建默认管理员失败: %w", err)
		}
	}

	log.Info().Msg("数据库连接和迁移成功 (WAL模式已启用)")
	return nil
}