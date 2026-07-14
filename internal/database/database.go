package database

import (
	"cloudstream/internal/models"
	"cloudstream/internal/utils"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
			PasswordVersion:       1,
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
	if err := migrateLegacyNotifications(); err != nil {
		return err
	}

	if err := migrateLegacyAdminPassword(); err != nil {
		return err
	}
	if err := resetAdminPasswordFromEnv(); err != nil {
		return err
	}

	log.Info().Msg("数据库连接和迁移成功 (WAL模式已启用)")
	return nil
}

func migrateLegacyNotifications() error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var users []models.User
		if err := tx.Order("id asc").Find(&users).Error; err != nil {
			return fmt.Errorf("读取旧版通知配置失败: %w", err)
		}
		for _, user := range users {
			fingerprint := legacyNotificationFingerprint(user)
			if user.NotificationMigrationFingerprint == fingerprint {
				continue
			}

			notifyType := user.NotifyType
			if notifyType != models.NotifyTypeTelegram {
				notifyType = models.NotifyTypeWebhook
			}
			webhookURL := strings.TrimSpace(user.WebhookURL)
			webhook := models.Notification{
				UserID: user.ID, Name: "旧版 Webhook", Type: models.NotifyTypeWebhook, LegacySource: models.NotifyTypeWebhook,
				WebhookURL: webhookURL, Enabled: webhookURL != "" && notifyType == models.NotifyTypeWebhook,
				NotifyOnComplete: user.NotifyOnComplete, NotifyOnError: user.NotifyOnError,
				NotifyOnStop: user.NotifyOnStop, NotifyOnManual: user.NotifyOnManual,
			}
			if err := syncLegacyNotification(tx, webhook, webhookURL != ""); err != nil {
				return err
			}
			telegramToken := strings.TrimSpace(user.TelegramToken)
			telegramChatID := strings.TrimSpace(user.TelegramChatID)
			telegramPresent := telegramToken != "" || telegramChatID != ""
			telegram := models.Notification{
				UserID: user.ID, Name: "旧版 Telegram", Type: models.NotifyTypeTelegram, LegacySource: models.NotifyTypeTelegram,
				TelegramToken: telegramToken, TelegramChatID: telegramChatID,
				Enabled:          telegramToken != "" && telegramChatID != "" && notifyType == models.NotifyTypeTelegram,
				NotifyOnComplete: user.NotifyOnComplete, NotifyOnError: user.NotifyOnError,
				NotifyOnStop: user.NotifyOnStop, NotifyOnManual: user.NotifyOnManual,
			}
			if err := syncLegacyNotification(tx, telegram, telegramPresent); err != nil {
				return err
			}
			if err := tx.Model(&models.User{}).Where("id = ?", user.ID).Update("notification_migration_fingerprint", fingerprint).Error; err != nil {
				return fmt.Errorf("保存通知迁移状态失败: %w", err)
			}
		}
		return nil
	})
}

func syncLegacyNotification(tx *gorm.DB, desired models.Notification, present bool) error {
	var stored models.Notification
	err := tx.Where("user_id = ? AND legacy_source = ?", desired.UserID, desired.LegacySource).First(&stored).Error
	if !present {
		if err == nil {
			if deleteErr := tx.Unscoped().Delete(&stored).Error; deleteErr != nil {
				return fmt.Errorf("删除已移除的旧版通知失败: %w", deleteErr)
			}
			return nil
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return fmt.Errorf("读取旧版通知迁移记录失败: %w", err)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		name, nameErr := availableLegacyNotificationName(tx, desired.UserID, desired.Name)
		if nameErr != nil {
			return nameErr
		}
		desired.Name = name
		desired.Version = 1
		requested := desired
		if createErr := tx.Create(&desired).Error; createErr != nil {
			return fmt.Errorf("迁移旧版通知配置失败: %w", createErr)
		}
		if updateErr := tx.Model(&desired).Updates(notificationMigrationUpdates(requested)).Error; updateErr != nil {
			return fmt.Errorf("保存旧版通知配置失败: %w", updateErr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取旧版通知迁移记录失败: %w", err)
	}
	desired.Name = stored.Name
	desired.Version = stored.Version + 1
	if updateErr := tx.Model(&stored).Updates(notificationMigrationUpdates(desired)).Error; updateErr != nil {
		return fmt.Errorf("同步旧版通知配置失败: %w", updateErr)
	}
	return nil
}

func availableLegacyNotificationName(tx *gorm.DB, userID uint, base string) (string, error) {
	for index := 0; ; index++ {
		candidate := base
		if index > 0 {
			candidate = fmt.Sprintf("%s (%d)", base, index+1)
		}
		var count int64
		if err := tx.Model(&models.Notification{}).Where("user_id = ? AND name = ?", userID, candidate).Count(&count).Error; err != nil {
			return "", fmt.Errorf("检查旧版通知名称失败: %w", err)
		}
		if count == 0 {
			return candidate, nil
		}
	}
}

func notificationMigrationUpdates(notification models.Notification) map[string]interface{} {
	return map[string]interface{}{
		"name": notification.Name, "type": notification.Type, "version": notification.Version,
		"webhook_url": notification.WebhookURL, "telegram_token": notification.TelegramToken,
		"telegram_chat_id": notification.TelegramChatID, "enabled": notification.Enabled,
		"notify_on_complete": notification.NotifyOnComplete, "notify_on_error": notification.NotifyOnError,
		"notify_on_stop": notification.NotifyOnStop, "notify_on_manual": notification.NotifyOnManual,
		"legacy_source": notification.LegacySource,
	}
}

func legacyNotificationFingerprint(user models.User) string {
	hash := sha256.New()
	for _, value := range []string{
		user.NotifyType, user.WebhookURL, user.TelegramToken, user.TelegramChatID,
		fmt.Sprintf("%t", user.NotifyOnComplete), fmt.Sprintf("%t", user.NotifyOnError),
		fmt.Sprintf("%t", user.NotifyOnStop), fmt.Sprintf("%t", user.NotifyOnManual),
	} {
		_, _ = fmt.Fprintf(hash, "%d:", len(value))
		_, _ = hash.Write([]byte(value))
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
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

func migrateLegacyAdminPassword() error {
	var admins []models.User
	if err := DB.Where("username = ? AND password_version = 0", "admin").Find(&admins).Error; err != nil {
		return fmt.Errorf("查询旧版管理员密码失败: %w", err)
	}

	for _, user := range admins {
		if !utils.CheckPasswordHash("admin", user.PasswordHash) {
			continue
		}

		newHash, err := utils.HashPassword(utils.SHA256Hex("admin"))
		if err != nil {
			return fmt.Errorf("升级旧版管理员密码失败: %w", err)
		}
		result := DB.Model(&models.User{}).
			Where("id = ? AND password_version = 0 AND password_hash = ?", user.ID, user.PasswordHash).
			Updates(map[string]interface{}{
				"password_hash":    newHash,
				"password_version": 1,
			})
		if result.Error != nil {
			return fmt.Errorf("更新旧版管理员密码失败: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("更新旧版管理员密码失败: 记录已发生变化")
		}
		log.Info().Str("username", user.Username).Msg("旧版管理员密码存储已升级")
	}
	return nil
}

func resetAdminPasswordFromEnv() error {
	password, ok := os.LookupEnv("CLOUDSTREAM_RESET_ADMIN_PASSWORD")
	if !ok {
		return nil
	}
	if password == "" {
		return fmt.Errorf("CLOUDSTREAM_RESET_ADMIN_PASSWORD 不能为空")
	}

	var users []models.User
	if err := DB.Order("id asc").Find(&users).Error; err != nil {
		return fmt.Errorf("读取管理员账户失败: %w", err)
	}
	if len(users) != 1 {
		return fmt.Errorf("重置管理员密码失败: 需要数据库中恰好存在一个用户，当前有 %d 个", len(users))
	}
	if err := resetUserPassword(users[0].ID, password); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "CloudStream admin password reset from CLOUDSTREAM_RESET_ADMIN_PASSWORD; remove the variable before the next startup.")
	return nil
}

func resetUserPassword(userID uint, password string) error {
	newHash, err := utils.HashPassword(utils.SHA256Hex(password))
	if err != nil {
		return fmt.Errorf("重置管理员密码失败: %w", err)
	}
	result := DB.Model(&models.User{}).
		Where("id = ?", userID).
		Updates(map[string]interface{}{
			"password_hash":           newHash,
			"password_version":        1,
			"token_version":           gorm.Expr("token_version + 1"),
			"needs_password_reminder": false,
			"password_reminder_shown": true,
		})
	if result.Error != nil {
		return fmt.Errorf("重置管理员密码失败: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("重置管理员密码失败: 管理员记录已发生变化")
	}
	return nil
}
