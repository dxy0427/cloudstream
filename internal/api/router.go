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
	r.Use(gin.Logger(), gin.Recovery())

	v1 := r.Group("/api/v1")
	{
		// 统一流地址 (STRM文件内指向的地址)
		// 同时允许 GET 和 HEAD，以便播放器探测
		v1.Match([]string{"GET", "HEAD"}, "/stream/s/*path", handlers.UnifiedStreamHandler)

		// 登录
		v1.POST("/login", auth.LoginHandler)

		// 需要登录的接口
		authorized := v1.Group("/")
		authorized.Use(auth.JWTAuthMiddleware())
		{
			authorized.GET("/username", handlers.GetUsernameHandler)
			authorized.POST("/update_credentials", handlers.UpdateCredentialsHandler)

			// 云账户管理
			authorized.POST("/accounts/test", handlers.TestAccountConnectionHandler)
			accounts := authorized.Group("/accounts")
			{
				accounts.GET("", handlers.ListAccountsHandler)
				accounts.POST("", handlers.CreateAccountHandler)
				accounts.PUT("/:id", handlers.UpdateAccountHandler)
				accounts.DELETE("/:id", handlers.DeleteAccountHandler)
			}

			// 任务管理
			tasks := authorized.Group("/tasks")
			{
				tasks.GET("", handlers.ListTasksHandler)
				tasks.POST("", handlers.CreateTaskHandler)
				tasks.PUT("/:id", handlers.UpdateTaskHandler)
				tasks.DELETE("/:id", handlers.DeleteTaskHandler)
				tasks.POST("/:id/run", handlers.ExecuteTaskHandler)
				tasks.POST("/:id/stop", handlers.StopTaskHandler)
			}

			// 云盘文件浏览
			cloud := authorized.Group("/cloud")
			{
				cloud.GET("/files", handlers.FileBrowserHandler)
			}
		}
	}

	// 静态文件服务 (适配 Vue Router History 模式)
	// 1. 明确映射 /assets 路径，确保 JS/CSS 能被找到
	r.Static("/assets", "./public/assets")

	// 2. 映射根目录下的特定文件 (如 favicon.ico)
	r.StaticFile("/favicon.ico", "./public/favicon.ico")

	// 3. 所有未匹配 API 的路由都处理为 SPA 回退
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		// 如果是 API 请求但没匹配到，返回 404 JSON
		if strings.HasPrefix(path, "/api") {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "API route not found"})
			return
		}

		// 检查请求的文件是否真实存在于 public 目录 (例如 /logo.png, /manifest.json)
		fullPath := filepath.Join("./public", path)
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			c.File(fullPath)
			return
		}

		// 否则返回 index.html，交给前端 Vue Router 处理
		c.File("./public/index.html")
	})

	return r
}