package mediaserver

import (
	"github.com/gin-gonic/gin"
)

// InitProxyRouter 初始化媒体服务器代理路由
func InitProxyRouter() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	r.Any("/*path", func(c *gin.Context) {
		serverID := GetManager().GetFirstEnabledServerID()
		if serverID == 0 {
			c.JSON(404, gin.H{"code": 1, "message": "没有启用的媒体服务器"})
			return
		}

		GetManager().HandleProxy(c, serverID, c.Request.URL.EscapedPath())
	})

	return r
}
