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

	// 确保数据库目录存在
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}

	// 连接 SQLite 数据库
	DB, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return fmt.Errorf("连接数据库失败: %w", err)
	}

	// 自动迁移结构
	err = DB.AutoMigrate(
		&models.User{},
		&models.Task{},
		&models.Account{},
	)
	if err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}

	// 如果没有用户，自动创建默认账号 admin/admin
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

	log.Info().Msg("数据库连接和迁移成功")
	return nil
}