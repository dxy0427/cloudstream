package api

import (
	"cloudstream/internal/api/handlers"
	"cloudstream/internal/auth"
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

	v1 := r.Group("/api/v1")
	{
		v1.Match([]string{"GET", "HEAD"}, "/stream/s/*path", handlers.UnifiedStreamHandler)
		v1.POST("/login", auth.LoginHandler)

		authorized := v1.Group("/")
		authorized.Use(auth.JWTAuthMiddleware())
		{
			authorized.GET("/username", handlers.GetUsernameHandler)
			authorized.GET("/logs", handlers.GetSystemLogsHandler) // <--- 新增日志接口路由
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