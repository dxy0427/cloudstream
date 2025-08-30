package handlers

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/utils"
	"github.com/gin-gonic/gin"
	"net/http"
)

// GetUsernameHandler：获取当前登录用户的用户名
func GetUsernameHandler(c *gin.Context) {
	username, _ := c.Get("username")
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"username": username}})
}

// UpdateCredentialsHandler：更新用户凭证（替代旧的ChangePasswordHandler，支持改用户名/密码）
func UpdateCredentialsHandler(c *gin.Context) {
	var req struct {
		NewUsername     string `json:"newUsername"`
		CurrentPassword string `json:"currentPassword" binding:"required"`
		NewPassword     string `json:"newPassword"`
		ConfirmPassword string `json:"confirmPassword"`
	}
	// 解析请求参数，缺失当前密码返回错误
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "当前密码不能为空"})
		return
	}

	// 根据Token获取当前登录用户
	currentUsername, _ := c.Get("username")
	var user models.User
	database.DB.Where("username = ?", currentUsername).First(&user)

	// 验证当前密码（所有修改的前提，不正确则返回401）
	if !utils.CheckPasswordHash(req.CurrentPassword, user.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 1, "message": "当前密码不正确"})
		return
	}

	changed := false

	// 处理用户名修改：非空且与原用户名不同时校验唯一性
	if req.NewUsername != "" && req.NewUsername != user.Username {
		var existingUser models.User
		if database.DB.Where("username = ?", req.NewUsername).First(&existingUser).Error == nil {
			c.JSON(http.StatusConflict, gin.H{"code": 1, "message": "新用户名已被占用"})
			return
		}
		user.Username = req.NewUsername
		changed = true
	}

	// 处理密码修改：非空时校验两次输入一致，加密后更新
	if req.NewPassword != "" {
		if req.NewPassword != req.ConfirmPassword {
			c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "两次输入的新密码不一致"})
			return
		}
		newPasswordHash, err := utils.HashPassword(req.NewPassword)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "密码加密失败"})
			return
		}
		user.PasswordHash = newPasswordHash
		changed = true
	}

	// 有修改时更新Token版本号（使旧Token失效）并保存
	if changed {
		user.TokenVersion++ // 安全机制：旧Token无法再使用
		if err := database.DB.Save(&user).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "更新凭证失败: " + err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "凭证更新成功，请重新登录"})
}
