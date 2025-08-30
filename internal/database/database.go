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

var DB *gorm.DB // GORM数据库实例（全局可用，已初始化完成后使用）

// ConnectDatabase：连接SQLite数据库，含目录创建、表迁移、默认管理员初始化
func ConnectDatabase(dbPath string) error {
	// 确保数据库目录存在（权限0755：所有者读写执行，其他读执行）
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}

	// 连接SQLite数据库，关闭GORM默认日志（避免冗余输出）
	DB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return fmt.Errorf("连接数据库失败: %w", err)
	}

	// 自动迁移表结构（User/Task/Account，不存在则创建，结构变更时更新）
	err = DB.AutoMigrate(&models.User{}, &models.Task{}, &models.Account{})
	if err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}

	// 检查是否有用户，无用户则创建默认管理员（admin/admin，密码加密存储）
	var userCount int64
	DB.Model(&models.User{}).Count(&userCount)
	if userCount == 0 {
		log.Info().Msg("未发现用户，正在创建默认管理员 admin/admin...")
		hashedPassword, _ := utils.HashPassword("admin") // 密码bcrypt加密
		defaultUser := models.User{Username: "admin", PasswordHash: hashedPassword}
		if err := DB.Create(&defaultUser).Error; err != nil {
			return fmt.Errorf("创建默认管理员失败: %w", err)
		}
	}

	log.Info().Msg("数据库连接和迁移成功")
	return nil
}