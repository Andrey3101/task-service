package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

// AuthMiddleware проверяет JWT токен и CSRF токен
func AuthMiddleware(authServiceURL string, redisClient *redis.Client, logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Получаем Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			c.Abort()
			return
		}

		// Получаем CSRF токен
		csrfToken := c.GetHeader("X-CSRF-Token")
		if csrfToken == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "CSRF token required"})
			c.Abort()
			return
		}

		// Извлекаем JWT токен
		token := strings.TrimPrefix(authHeader, "Bearer ")

		// Проверяем токен через auth-service
		userData, err := validateTokenWithAuthService(authServiceURL, token, csrfToken, redisClient, logger)
		if err != nil {
			logger.Error("Token validation failed", zap.Error(err))
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		// Сохраняем данные пользователя в контексте
		c.Set("user_id", userData.UserID)
		c.Set("role", userData.Role)
		c.Set("company_id", userData.CompanyID)
		c.Set("email", userData.Email)

		c.Next()
	}
}

// UserData структура данных пользователя
type UserData struct {
	UserID    string `json:"user_id"`
	Role      string `json:"role"`
	CompanyID string `json:"company_id"`
	Email     string `json:"email"`
}

// validateTokenWithAuthService проверяет токен через auth-service
func validateTokenWithAuthService(authServiceURL, token, csrfToken string, redisClient *redis.Client, logger *zap.Logger) (*UserData, error) {
	// Создаем HTTP клиент с таймаутом
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Формируем запрос к auth-service
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/profile", authServiceURL), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Добавляем заголовки
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("X-CSRF-Token", csrfToken)
	req.Header.Set("Content-Type", "application/json")

	// Выполняем запрос
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request to auth-service: %w", err)
	}
	defer resp.Body.Close()

	// Проверяем статус ответа
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth-service returned status: %d", resp.StatusCode)
	}

	// Декодируем ответ
	var userData UserData
	if err := json.NewDecoder(resp.Body).Decode(&userData); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Проверяем CSRF токен в Redis
	if err := validateCSRFToken(redisClient, userData.UserID, csrfToken); err != nil {
		return nil, fmt.Errorf("CSRF validation failed: %w", err)
	}

	return &userData, nil
}

// validateCSRFToken проверяет CSRF токен в Redis
func validateCSRFToken(redisClient *redis.Client, userID, csrfToken string) error {
	ctx := redisClient.Context()

	// Получаем сохраненный CSRF токен
	storedToken, err := redisClient.Get(ctx, fmt.Sprintf("csrf:%s", userID)).Result()
	if err != nil {
		if err == redis.Nil {
			return fmt.Errorf("CSRF token not found")
		}
		return fmt.Errorf("failed to get CSRF token from Redis: %w", err)
	}

	// Сравниваем токены
	if storedToken != csrfToken {
		return fmt.Errorf("CSRF token mismatch")
	}

	return nil
}

// RequireRole middleware для проверки роли пользователя
func RequireRole(requiredRole string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole, exists := c.Get("role")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User role not found"})
			c.Abort()
			return
		}

		if userRole != requiredRole {
			c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireCompany middleware для проверки принадлежности к компании
func RequireCompany() gin.HandlerFunc {
	return func(c *gin.Context) {
		companyID, exists := c.Get("company_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Company ID not found"})
			c.Abort()
			return
		}

		if companyID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Company ID is required"})
			c.Abort()
			return
		}

		c.Next()
	}
}
