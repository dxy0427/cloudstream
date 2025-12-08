package handlers

import (
	"cloudstream/internal/database"
	"cloudstream/internal/models"
	"cloudstream/internal/utils"
	"github.com/gin-gonic/gin"
	"net/http"
)

// GetUsernameHandler
func GetUsernameHandler(c *gin.Context) {
	username, _ := c.Get("username")
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"username": username}})
}

// UpdateCredentialsHandler
func UpdateCredentialsHandler(c *gin.Context) {
	var req struct {
		NewUsername     string `json:"newUsername"`
		CurrentPassword string `json:"currentPassword" binding:"required"`
		NewPassword     string `json:"newPassword"`
		ConfirmPassword string `json:"confirmPassword"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "当前密码不能为空"})
		return
	}

	currentUsername, _ := c.Get("username")
	var user models.User
	database.DB.Where("username = ?", currentUsername).First(&user)

	if !utils.CheckPasswordHash(req.CurrentPassword, user.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 1, "message": "当前密码不正确"})
		return
	}

	changed := false

	if req.NewUsername != "" && req.NewUsername != user.Username {
		var existingUser models.User
		if database.DB.Where("username = ?", req.NewUsername).First(&existingUser).Error == nil {
			c.JSON(http.StatusConflict, gin.H{"code": 1, "message": "新用户名已被占用"})
			return
		}
		user.Username = req.NewUsername
		changed = true
	}

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

	if changed {
		user.TokenVersion++
		if err := database.DB.Save(&user).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "更新凭证失败: " + err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "凭证更新成功，请重新登录"})
}