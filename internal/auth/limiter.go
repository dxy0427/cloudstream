package auth

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"sync"
	"time"
)

// 简单的内存限流器
var (
	ipStore = make(map[string]*rateLimiter)
	mu      sync.Mutex
)

type rateLimiter struct {
	count    int
	lastTime time.Time
}

// LoginRateLimiter 每分钟只允许尝试 5 次登录
func LoginRateLimiter() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		mu.Lock()
		defer mu.Unlock()

		limiter, exists := ipStore[ip]
		if !exists {
			limiter = &rateLimiter{count: 0, lastTime: time.Now()}
			ipStore[ip] = limiter
		}

		// 如果距离上次重置超过1分钟，重置计数器
		if time.Since(limiter.lastTime) > time.Minute {
			limiter.count = 0
			limiter.lastTime = time.Now()
		}

		limiter.count++

		if limiter.count > 5 {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "尝试次数过多，请 1 分钟后再试"})
			c.Abort()
			return
		}

		c.Next()
	}
}