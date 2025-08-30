package handlers

import (
	"cloudstream/internal/models"
	"cloudstream/internal/pan123"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
)

// TestAccountConnectionHandler：处理账户连接测试请求（校验pan123账户凭证有效性）
func TestAccountConnectionHandler(c *gin.Context) {
	var account models.Account
	// 绑定请求JSON中的账户凭证（如用户名、密码等）
	if err := c.ShouldBindJSON(&account); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "账户凭证无效"})
		return
	}

	// 初始化pan123客户端（传入待测试的账户信息）
	client := pan123.NewClient(account)
	// 测试获取AccessToken（核心校验逻辑：验证账户能否正常连接pan123服务）
	_, err := client.GetAccessTokenForTest()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": fmt.Sprintf("连接失败: %s", err.Error())})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "连接成功！"})
}
