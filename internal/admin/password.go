package admin

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/utils"
	"fmt"
	"io"

	"gorm.io/gorm"
)

const passwordCommandUsage = "用法: cloudstream admin password [NEW_PASSWORD]"

func Run(args []string, dbPath string, output io.Writer) error {
	if len(args) < 2 || len(args) > 3 || args[0] != "admin" || args[1] != "password" {
		return fmt.Errorf("%s", passwordCommandUsage)
	}

	generated := len(args) == 2
	password := ""
	if generated {
		var err error
		password, err = utils.GenerateRandomPassword()
		if err != nil {
			return fmt.Errorf("生成随机密码失败: %w", err)
		}
	} else {
		password = args[2]
	}

	var beforeUpdate func(string) error
	if generated {
		beforeUpdate = func(string) error {
			if _, err := fmt.Fprintf(output, "新密码（仅显示一次）：%s\n", password); err != nil {
				return fmt.Errorf("输出随机密码失败: %w", err)
			}
			return nil
		}
	}

	username, err := resetPassword(dbPath, password, beforeUpdate)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(output, "管理员 %q 密码已重置，已有登录会话将失效。\n", username); err != nil {
		return fmt.Errorf("输出重置结果失败: %w", err)
	}
	return nil
}

func ResetPassword(dbPath, password string) (string, error) {
	return resetPassword(dbPath, password, nil)
}

func resetPassword(dbPath, password string, beforeUpdate func(string) error) (string, error) {
	if password == "" {
		return "", fmt.Errorf("新密码不能为空")
	}

	db, err := database.OpenExistingDatabase(dbPath)
	if err != nil {
		return "", err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return "", fmt.Errorf("获取数据库连接失败: %w", err)
	}
	defer sqlDB.Close()

	var users []models.User
	if err := db.Order("id asc").Limit(2).Find(&users).Error; err != nil {
		return "", fmt.Errorf("读取管理员失败: %w", err)
	}
	if len(users) == 0 {
		return "", fmt.Errorf("数据库中没有管理员")
	}
	if len(users) != 1 {
		return "", fmt.Errorf("数据库中存在多个用户，无法确定要重置的管理员")
	}

	user := users[0]
	passwordHash, err := utils.HashPassword(utils.SHA256Hex(password))
	if err != nil {
		return "", fmt.Errorf("密码加密失败: %w", err)
	}
	if beforeUpdate != nil {
		if err := beforeUpdate(user.Username); err != nil {
			return "", err
		}
	}
	result := db.Model(&models.User{}).
		Where("id = ? AND password_hash = ? AND token_version = ?", user.ID, user.PasswordHash, user.TokenVersion).
		Updates(map[string]any{
			"password_hash":           passwordHash,
			"token_version":           gorm.Expr("token_version + 1"),
			"needs_password_reminder": false,
			"password_reminder_shown": true,
		})
	if result.Error != nil {
		return "", fmt.Errorf("重置管理员密码失败: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return "", fmt.Errorf("管理员凭证已发生变化，请重试")
	}
	return user.Username, nil
}
