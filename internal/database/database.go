package database

import (
	"cloudstream/internal/models"
	"cloudstream/internal/utils"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func ConnectDatabase(dbPath string) error {
	var err error

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}

	dbConfig := &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		TranslateError: true,
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
		&models.Notification{},
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
		initialPassword, generated, err := initialAdminPassword()
		if err != nil {
			return err
		}
		hashedPassword, err := utils.HashPassword(utils.SHA256Hex(initialPassword))
		if err != nil {
			return fmt.Errorf("密码哈希失败: %w", err)
		}
		defaultUser := models.User{
			Username:              "admin",
			PasswordHash:          hashedPassword,
			TokenVersion:          1,
			NeedsPasswordReminder: true,
		}
		if err := DB.Create(&defaultUser).Error; err != nil {
			return fmt.Errorf("创建默认管理员失败: %w", err)
		}
		log.Info().Msg("已创建初始管理员账户")
		if generated {
			fmt.Fprintf(os.Stderr, "\nCloudStream initial admin password (shown once): %s\n\n", initialPassword)
		}
	}
	log.Info().Msg("数据库连接和迁移成功 (WAL模式已启用)")
	return nil
}

func initialAdminPassword() (string, bool, error) {
	if password, ok := os.LookupEnv("CLOUDSTREAM_ADMIN_PASSWORD"); ok {
		if password == "" {
			return "", false, fmt.Errorf("CLOUDSTREAM_ADMIN_PASSWORD 不能为空")
		}
		return password, false, nil
	}

	randomBytes := make([]byte, 24)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", false, fmt.Errorf("生成初始管理员密码失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(randomBytes), true, nil
}
