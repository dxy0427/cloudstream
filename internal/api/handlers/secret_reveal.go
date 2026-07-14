package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func validateSecretRevealTimestamp(c *gin.Context, requested string, actual time.Time, conflictMessage string) bool {
	expected, err := time.Parse(time.RFC3339Nano, requested)
	if err != nil || !actual.Equal(expected) {
		c.JSON(http.StatusConflict, gin.H{"code": 1, "message": conflictMessage})
		return false
	}
	return true
}

func respondSecretValue(c *gin.Context, field, value, missingMessage string) {
	if value == "" {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": missingMessage})
		return
	}
	c.Header("Cache-Control", "no-store, max-age=0")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Referrer-Policy", "no-referrer")
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"field": field, "value": value}})
}
