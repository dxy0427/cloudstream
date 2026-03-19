package auth

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"sync"
	"time"
)

const (
	loginMaxAttempts = 5
	loginWindow      = time.Minute
	entryTTL         = 5 * time.Minute
)

type rateLimiter struct {
	count       int
	windowStart time.Time
	lastSeen    time.Time
}

var loginLimiterStore = struct {
	sync.Mutex
	entries map[string]*rateLimiter
}{
	entries: make(map[string]*rateLimiter),
}

func cleanupExpiredEntries(now time.Time) {
	for ip, limiter := range loginLimiterStore.entries {
		if now.Sub(limiter.lastSeen) > entryTTL {
			delete(loginLimiterStore.entries, ip)
		}
	}
}

func LoginRateLimiter() gin.HandlerFunc {
	return func(c *gin.Context) {
		now := time.Now()
		ip := c.ClientIP()

		loginLimiterStore.Lock()
		defer loginLimiterStore.Unlock()

		cleanupExpiredEntries(now)

		limiter, exists := loginLimiterStore.entries[ip]
		if !exists {
			limiter = &rateLimiter{windowStart: now, lastSeen: now}
			loginLimiterStore.entries[ip] = limiter
		}

		if now.Sub(limiter.windowStart) >= loginWindow {
			limiter.count = 0
			limiter.windowStart = now
		}

		limiter.count++
		limiter.lastSeen = now

		if limiter.count > loginMaxAttempts {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "尝试次数过多，请 1 分钟后再试"})
			c.Abort()
			return
		}

		c.Next()
	}
}
