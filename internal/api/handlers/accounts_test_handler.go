package handlers

import (
	"cloudstream/internal/models"
	"cloudstream/internal/pan123"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
)

// TestAccountConnectionHandler：测试pan123云账户连接是否有效
func TestAccountConnectionHandler(c *gin.Context) {
	var account models.Account
	// 解析请求中的账户凭证
	if err := c.ShouldBindJSON(&account); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "账户凭证无效"})
		return
	}

	// 初始化pan123客户端
	client := pan123.NewClient(account)
	// 测试获取AccessToken，验证连接
	_, err := client.GetAccessTokenForTest()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": fmt.Sprintf("连接失败: %s", err.Error())})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "连接成功！"})
}
