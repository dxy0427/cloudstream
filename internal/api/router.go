package api

import (
	"cloudstream/internal/api/handlers"
	"cloudstream/internal/auth"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func InitRouter() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// 1. 性能优化：开启 Gzip 压缩 (大幅减少 JSON 体积)
	r.Use(gzip.Gzip(gzip.DefaultCompression))

	v1 := r.Group("/api/v1")
	{
		// 公开接口
		v1.Match([]string{"GET", "HEAD"}, "/stream/s/*path", handlers.UnifiedStreamHandler)
		v1.POST("/login", auth.LoginRateLimiter(), auth.LoginHandler)

		// 鉴权接口
		authorized := v1.Group("/")
		authorized.Use(auth.JWTAuthMiddleware())
		{
			// 2. 安全优化：新增主动登出接口
			authorized.POST("/logout", handlers.LogoutHandler)

			authorized.GET("/username", handlers.GetUsernameHandler)
			authorized.GET("/logs", handlers.GetSystemLogsHandler)
			
			authorized.POST("/webhook/test", handlers.TestWebhookHandler)
			authorized.POST("/notifications", handlers.UpdateNotificationHandler)
			authorized.POST("/update_credentials", handlers.UpdateCredentialsHandler)
			authorized.POST("/accounts/test", handlers.TestAccountConnectionHandler)

			accounts := authorized.Group("/accounts")
			{
				accounts.GET("", handlers.ListAccountsHandler)
				accounts.POST("", handlers.CreateAccountHandler)
				accounts.PUT("/:id", handlers.UpdateAccountHandler)
				accounts.DELETE("/:id", handlers.DeleteAccountHandler)
			}

			tasks := authorized.Group("/tasks")
			{
				tasks.GET("", handlers.ListTasksHandler)
				tasks.POST("", handlers.CreateTaskHandler)
				tasks.PUT("/:id", handlers.UpdateTaskHandler)
				tasks.DELETE("/:id", handlers.DeleteTaskHandler)
				tasks.POST("/:id/run", handlers.ExecuteTaskHandler)
				tasks.POST("/:id/stop", handlers.StopTaskHandler)
			}

			cloud := authorized.Group("/cloud")
			{
				cloud.GET("/files", handlers.FileBrowserHandler)
			}
		}
	}

	// 静态文件服务
	r.Static("/assets", "./public/assets")
	r.StaticFile("/favicon.ico", "./public/favicon.ico")

	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api") {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "API route not found"})
			return
		}
		fullPath := filepath.Join("./public", path)
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			c.File(fullPath)
			return
		}
		c.File("./public/index.html")
	})

	return r
}