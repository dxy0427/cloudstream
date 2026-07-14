package auth

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
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
	entries     map[string]*rateLimiter
	nextCleanup time.Time
}{
	entries:     make(map[string]*rateLimiter),
	nextCleanup: time.Now().Add(entryTTL),
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

		if !now.Before(loginLimiterStore.nextCleanup) {
			cleanupExpiredEntries(now)
			loginLimiterStore.nextCleanup = now.Add(entryTTL)
		}

		limiter, exists := loginLimiterStore.entries[ip]
		if !exists {
			limiter = &rateLimiter{windowStart: now, lastSeen: now}
			loginLimiterStore.entries[ip] = limiter
		}

		if now.Sub(limiter.windowStart) >= loginWindow {
			limiter.count = 0
			limiter.windowStart = now
		}

		limiter.lastSeen = now

		if limiter.count >= loginMaxAttempts {
			loginLimiterStore.Unlock()
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "尝试次数过多，请 1 分钟后再试"})
			c.Abort()
			return
		}

		// 在进入登录处理器前预占一次额度，避免并发请求同时绕过上限。
		limiter.count++
		loginLimiterStore.Unlock()

		c.Next()

		if c.Writer.Status() >= http.StatusOK && c.Writer.Status() < http.StatusMultipleChoices {
			loginLimiterStore.Lock()
			if current := loginLimiterStore.entries[ip]; current == limiter {
				delete(loginLimiterStore.entries, ip)
			}
			loginLimiterStore.Unlock()
		} else if c.Writer.Status() != http.StatusUnauthorized {
			loginLimiterStore.Lock()
			if current := loginLimiterStore.entries[ip]; current == limiter && current.count > 0 {
				current.count--
				current.lastSeen = time.Now()
			}
			loginLimiterStore.Unlock()
		}
	}
}
