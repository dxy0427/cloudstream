package api

import (
	"cloudstream/internal/api/handlers"
	"cloudstream/internal/auth"
	"context"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

const (
	maxControlJSONBytes  = 1 << 20
	maxSSEConnections    = 8
	sessionCheckInterval = 10 * time.Second
)

type sseConnectionLimiter struct {
	slots chan struct{}
}

type synchronizedSSEWriter struct {
	gin.ResponseWriter
	mu      sync.Mutex
	pending []byte
	cancel  context.CancelFunc
}

func (writer *synchronizedSSEWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func InitRouter() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	if err := r.SetTrustedProxies(nil); err != nil {
		panic(err)
	}
	r.Use(gin.Recovery())
	r.Use(limitControlJSONBody())

	// 排除流媒体、媒体代理与 SSE，避免 gzip 破坏 Range/二进制/长连接
	r.Use(gzip.Gzip(gzip.DefaultCompression, gzip.WithExcludedPaths([]string{
		"/api/v1/stream",
		"/api/v1/ms",
		"/api/v1/logs/stream",
		"/api/v1/tasks/stream",
	})))

	r.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		host := c.Request.Host

		if origin == "" || origin == "http://"+host || origin == "https://"+host {
			c.Header("Access-Control-Allow-Origin", origin)
		}
		c.Header("Vary", "Origin")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, HEAD")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Max-Age", "86400")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	})
	logStreams := &sseConnectionLimiter{slots: make(chan struct{}, maxSSEConnections)}
	taskStreams := &sseConnectionLimiter{slots: make(chan struct{}, maxSSEConnections)}

	v1 := r.Group("/api/v1")
	{
		v1.Match([]string{"GET", "HEAD"}, "/stream/s/*path", handlers.UnifiedStreamHandler)
		v1.POST("/login", auth.LoginRateLimiter(), auth.LoginHandler)

		authorized := v1.Group("/")
		authorized.Use(auth.JWTAuthMiddleware())
		{
			authorized.POST("/logout", handlers.LogoutHandler)

			authorized.GET("/username", handlers.GetUsernameHandler)
			authorized.GET("/dashboard/stats", handlers.GetDashboardStatsHandler)
			authorized.GET("/logs", handlers.GetSystemLogsHandler)
			authorized.GET("/logs/stream", logStreams.guard(false), handlers.StreamSystemLogsHandler)

			authorized.GET("/user/settings", handlers.GetUserSettingsHandler)
			authorized.POST("/user/settings", handlers.UpdateUserSettingsHandler)
			authorized.POST("/user/password-reminder/dismiss", handlers.DismissPasswordReminderHandler)

			authorized.POST("/update_credentials", handlers.UpdateCredentialsHandler)
			authorized.POST("/accounts/test", handlers.TestAccountConnectionHandler)

			accounts := authorized.Group("/accounts")
			{
				accounts.GET("", handlers.ListAccountsHandler)
				accounts.GET("/:id", handlers.GetAccountHandler)
				accounts.POST("", handlers.CreateAccountHandler)
				accounts.PUT("/:id", handlers.UpdateAccountHandler)
				accounts.DELETE("/:id", handlers.DeleteAccountHandler)
			}

			notifications := authorized.Group("/notifications")
			{
				notifications.GET("", handlers.ListNotificationsHandler)
				notifications.GET("/:id", handlers.GetNotificationHandler)
				notifications.POST("", handlers.CreateNotificationHandler)
				notifications.POST("/test", handlers.TestNotificationHandler)
				notifications.PUT("/:id", handlers.UpdateNotificationHandler)
				notifications.DELETE("/:id", handlers.DeleteNotificationHandler)
			}

			tasks := authorized.Group("/tasks")
			{
				tasks.GET("", handlers.ListTasksHandler)
				tasks.GET("/stream", taskStreams.guard(true), handlers.StreamTasksHandler)
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

			mediaservers := authorized.Group("/mediaservers")
			{
				mediaservers.GET("", handlers.ListMediaServersHandler)
				mediaservers.GET("/:id", handlers.GetMediaServerHandler)
				mediaservers.POST("", handlers.CreateMediaServerHandler)
				mediaservers.PUT("/:id", handlers.UpdateMediaServerHandler)
				mediaservers.DELETE("/:id", handlers.DeleteMediaServerHandler)
				mediaservers.POST("/test", handlers.TestMediaServerConnectionHandler)
			}
		}
	}

	v1.Any("/ms/:id/*path", handlers.MediaServerProxyHandler)

	r.Match([]string{"GET", "HEAD"}, "/assets/*filepath", func(c *gin.Context) {
		assetPath := "assets/" + strings.TrimPrefix(c.Param("filepath"), "/")
		if !servePublicFile(c, assetPath) {
			c.Status(http.StatusNotFound)
		}
	})

	r.NoRoute(func(c *gin.Context) {
		reqPath := c.Request.URL.Path
		if strings.HasPrefix(reqPath, "/api") {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "API route not found"})
			return
		}
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Status(http.StatusNotFound)
			return
		}
		if servePublicFile(c, strings.TrimPrefix(reqPath, "/")) {
			return
		}
		if !servePublicFile(c, "index.html") {
			c.Status(http.StatusNotFound)
		}
	})

	return r
}

func limitControlJSONBody() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body == nil || c.Request.Body == http.NoBody || !strings.HasPrefix(c.Request.URL.Path, "/api/") || isPublicStreamPath(c.Request.URL.Path) {
			c.Next()
			return
		}
		mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if err != nil || (mediaType != "application/json" && !strings.HasSuffix(mediaType, "+json")) {
			c.Next()
			return
		}
		controller := http.NewResponseController(c.Writer)
		_ = controller.SetReadDeadline(time.Now().Add(30 * time.Second))
		defer controller.SetReadDeadline(time.Time{})
		if c.Request.ContentLength > maxControlJSONBytes {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"code": 1, "message": "请求体不能超过 1 MiB"})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxControlJSONBytes)
		c.Next()
	}
}

func isPublicStreamPath(requestPath string) bool {
	return requestPath == "/api/v1/stream" ||
		strings.HasPrefix(requestPath, "/api/v1/stream/") ||
		requestPath == "/api/v1/ms" ||
		strings.HasPrefix(requestPath, "/api/v1/ms/")
}

func (limiter *sseConnectionLimiter) guard(sendHeartbeat bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		select {
		case limiter.slots <- struct{}{}:
			defer func() { <-limiter.slots }()
		default:
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"code": 1, "message": "SSE 连接数已达上限"})
			return
		}

		if sendHeartbeat {
			c.Writer.Header().Set("Content-Type", "text/event-stream")
			c.Writer.Header().Set("Cache-Control", "no-cache")
			c.Writer.Header().Set("Connection", "keep-alive")
			c.Writer.Header().Set("X-Accel-Buffering", "no")
			c.Writer.WriteHeaderNow()
		}

		streamContext, cancel := context.WithCancel(c.Request.Context())
		c.Request = c.Request.WithContext(streamContext)
		writer := &synchronizedSSEWriter{ResponseWriter: c.Writer, cancel: cancel}
		c.Writer = writer

		monitorContext := c.Copy()
		done := make(chan struct{})
		monitorStopped := make(chan struct{})
		go monitorSSESession(monitorContext, writer, cancel, done, monitorStopped, sendHeartbeat)
		defer func() {
			close(done)
			cancel()
			<-monitorStopped
		}()
		c.Next()
	}
}

func monitorSSESession(c *gin.Context, writer *synchronizedSSEWriter, cancel context.CancelFunc, done <-chan struct{}, stopped chan<- struct{}, sendHeartbeat bool) {
	ticker := time.NewTicker(sessionCheckInterval)
	defer ticker.Stop()
	defer close(stopped)
	for {
		select {
		case <-done:
			return
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			if !auth.SessionStillValid(c) {
				cancel()
				return
			}
			select {
			case <-done:
				return
			default:
			}
			if sendHeartbeat {
				writer.writeHeartbeat()
			}
		}
	}
}

func (writer *synchronizedSSEWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.pending = append(writer.pending, data...)
	return len(data), nil
}

func (writer *synchronizedSSEWriter) WriteString(data string) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.pending = append(writer.pending, data...)
	return len(data), nil
}

func (writer *synchronizedSSEWriter) Flush() {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	controller := http.NewResponseController(writer.ResponseWriter)
	if err := controller.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		writer.cancel()
		return
	}
	defer controller.SetWriteDeadline(time.Time{})
	if err := writer.flushPendingLocked(); err != nil {
		writer.cancel()
		return
	}
	if err := controller.Flush(); err != nil {
		writer.cancel()
	}
}

func (writer *synchronizedSSEWriter) writeHeartbeat() {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if len(writer.pending) != 0 {
		return
	}
	controller := http.NewResponseController(writer.ResponseWriter)
	if err := controller.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		writer.cancel()
		return
	}
	defer controller.SetWriteDeadline(time.Time{})
	if _, err := writer.ResponseWriter.Write([]byte(": ping\n\n")); err != nil {
		writer.cancel()
		return
	}
	if err := controller.Flush(); err != nil {
		writer.cancel()
	}
}

func (writer *synchronizedSSEWriter) flushPendingLocked() error {
	for len(writer.pending) > 0 {
		written, err := writer.ResponseWriter.Write(writer.pending)
		writer.pending = writer.pending[written:]
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func servePublicFile(c *gin.Context, requestedPath string) bool {
	cleanPath := path.Clean(strings.TrimPrefix(requestedPath, "/"))
	if cleanPath == "." || cleanPath == ".." || path.IsAbs(cleanPath) || strings.HasPrefix(cleanPath, "../") {
		return false
	}

	root, err := os.OpenRoot("./public")
	if err != nil {
		return false
	}
	defer root.Close()
	file, err := root.Open(cleanPath)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if contentType := mime.TypeByExtension(path.Ext(cleanPath)); contentType != "" {
		c.Header("Content-Type", contentType)
	}
	http.ServeContent(c.Writer, c.Request, path.Base(cleanPath), info.ModTime(), file)
	return true
}
