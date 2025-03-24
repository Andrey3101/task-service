package middleware

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/andre/task-service/config"
	"github.com/gin-gonic/gin"
)

func AuthMiddleware(cfg config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			c.Abort()
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token format"})
			c.Abort()
			return
		}

		client := &http.Client{Timeout: 5 * time.Second} // Добавлен тайм-аут
		req, err := http.NewRequest("GET", cfg.AuthService+"/profile", nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create request"})
			c.Abort()
			return
		}
		req.Header.Set("Authorization", authHeader)
		resp, err := client.Do(req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to validate token: " + err.Error()})
			c.Abort()
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token validation failed"})
			c.Abort()
			return
		}

		var user struct {
			UserID string `json:"user_id"`
			Role   string `json:"role"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token response"})
			c.Abort()
			return
		}
		c.Set("user_id", user.UserID)
		c.Set("role", user.Role)
		c.Next()
	}
}
