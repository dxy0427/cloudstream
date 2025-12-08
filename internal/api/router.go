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
	// 设置为发布模式，减少 Gin 内部的调试输出
	gin.SetMode(gin.ReleaseMode)
	
	r := gin.New()
	
	// 优化：移除 gin.Logger()，只保留 Recovery (崩溃恢复)
	// 这样就不会有烦人的 [GIN] 200 | ... 访问日志了
	r.Use(gin.Recovery())

	v1 := r.Group("/api/v1")
	{
		v1.Match([]string{"GET", "HEAD"}, "/stream/s/*path", handlers.UnifiedStreamHandler)
		v1.POST("/login", auth.LoginHandler)

		authorized := v1.Group("/")
		authorized.Use(auth.JWTAuthMiddleware())
		{
			authorized.GET("/username", handlers.GetUsernameHandler)
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