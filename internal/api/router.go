package api

import (
	"cloudstream/internal/api/handlers"
	"cloudstream/internal/auth"
	"github.com/gin-gonic/gin"
)

// InitRouter：初始化Gin路由，配置API接口、JWT权限中间件及静态资源
func InitRouter() *gin.Engine {
	// 设置Gin为发布模式，关闭调试日志
	gin.SetMode(gin.ReleaseMode)
	// 新建路由引擎，使用日志和异常恢复中间件
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	// API v1版本分组：/api/v1
	v1 := r.Group("/api/v1")
	{
		// 无需认证的接口
		v1.GET("/stream/:accountID/:fileID", handlers.StreamHandler) // 文件播放接口（重定向到下载链接）
		v1.POST("/login", auth.LoginHandler)                        // 登录接口（获取JWT Token）

		// 需JWT认证的接口分组（通过JWTAuthMiddleware校验Token）
		authorized := v1.Group("/")
		authorized.Use(auth.JWTAuthMiddleware())
		{
			// 个人凭证相关
			authorized.GET("/username", handlers.GetUsernameHandler)       // 获取当前登录用户名
			authorized.POST("/update_credentials", handlers.UpdateCredentialsHandler) // 更新用户名/密码

			// 云账户相关
			authorized.POST("/accounts/test", handlers.TestAccountConnectionHandler) // 测试云账户连接
			accounts := authorized.Group("/accounts")                               // 云账户CRUD接口
			{
				accounts.GET("", handlers.ListAccountsHandler)   // 查询所有账户
				accounts.POST("", handlers.CreateAccountHandler) // 新增账户
				accounts.PUT("/:id", handlers.UpdateAccountHandler) // 更新账户
				accounts.DELETE("/:id", handlers.DeleteAccountHandler) // 删除账户
			}

			// 任务相关
			tasks := authorized.Group("/tasks") // 任务管理接口
			{
				tasks.GET("", handlers.ListTasksHandler)   // 查询所有任务（含运行状态）
				tasks.POST("", handlers.CreateTaskHandler) // 新增任务
				tasks.DELETE("/:id", handlers.DeleteTaskHandler) // 删除任务
				tasks.POST("/:id/run", handlers.ExecuteTaskHandler) // 手动执行任务
				tasks.POST("/:id/stop", handlers.StopTaskHandler)   // 停止任务
			}

			// 云盘操作相关
			cloud := authorized.Group("/cloud") // 云盘浏览接口
			{
				cloud.GET("/files", handlers.FileBrowserHandler) // 浏览云盘文件列表
			}
		}
	}

	// 静态资源：默认首页（前端SPA入口）
	r.StaticFile("/", "./public/index.html")
	// 404路由重定向到首页（适配前端SPA路由，避免刷新404）
	r.NoRoute(func(c *gin.Context) {
		c.File("./public/index.html")
	})

	return r
}