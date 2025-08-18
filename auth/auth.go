package auth

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/andre/task-service/config"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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

		client := &http.Client{Timeout: 5 * time.Second}
		req, err := http.NewRequest("GET", cfg.Auth.ServiceURL+"/profile", nil)
		if err != nil {
			log.Printf("Failed to create request to auth-service: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create request"})
			c.Abort()
			return
		}
		req.Header.Set("Authorization", authHeader)
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Failed to validate token with auth-service: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to validate token: " + err.Error()})
			c.Abort()
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Printf("Auth-service returned non-OK status: %d", resp.StatusCode)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token validation failed"})
			c.Abort()
			return
		}

		var user struct {
			UserID    string    `json:"user_id"`
			Role      string    `json:"role"`
			CompanyID uuid.UUID `json:"company_id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
			log.Printf("Failed to decode auth-service response: %v", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token response"})
			c.Abort()
			return
		}

		// Установка значений в контекст
		c.Set("user_id", user.UserID)
		c.Set("role", user.Role)
		c.Set("company_id", user.CompanyID.String()) // Сохраняем как строку для совместимости с GetString

		c.Next()
	}
}
