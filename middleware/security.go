package middleware

import (
	"github.com/gin-gonic/gin"
)

// Security middleware для базовых настроек безопасности
func Security() gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		// Заголовки безопасности
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		c.Next()
	})
}
