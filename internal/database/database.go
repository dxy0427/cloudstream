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

	dbConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	}

	DB, err = gorm.Open(sqlite.Open(dbPath), dbConfig)
	if err != nil {
		return fmt.Errorf("连接数据库失败: %w", err)
	}

	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}

	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)

	if _, err := sqlDB.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		log.Warn().Err(err).Msg("开启 SQLite WAL 模式失败，性能可能受限")
	}
	if _, err := sqlDB.Exec("PRAGMA busy_timeout=5000;"); err != nil {
		log.Warn().Err(err).Msg("设置 busy_timeout 失败")
	}

	err = DB.AutoMigrate(
		&models.User{},
		&models.Task{},
		&models.Account{},
		&models.TaskFile{},
		&models.MediaServer{},
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
		hashedPassword, err := utils.HashPassword(utils.SHA256Hex("admin"))
		if err != nil {
			return fmt.Errorf("密码哈希失败: %w", err)
		}
		defaultUser := models.User{
			Username:          "admin",
			PasswordHash:      hashedPassword,
			TokenVersion:      1,
			PasswordVersion:   1,
			NeedsPasswordReminder: true,
		}
		if err := DB.Create(&defaultUser).Error; err != nil {
			return fmt.Errorf("创建默认管理员失败: %w", err)
		}
	}

	var oldVersionUsers []models.User
	if err := DB.Where("password_version = 0").Find(&oldVersionUsers).Error; err == nil {
		for _, u := range oldVersionUsers {
			if u.Username == "admin" {
				newHash, err := utils.HashPassword(utils.SHA256Hex("admin"))
				if err == nil {
					DB.Model(&u).Updates(map[string]interface{}{
						"password_hash":    newHash,
						"password_version": 1,
					})
					log.Info().Str("username", "admin").Msg("默认管理员密码已升级到新方案(SHA-256+bcrypt)")
				}
			}
		}
	}

	log.Info().Msg("数据库连接和迁移成功 (WAL模式已启用)")
	return nil
}
