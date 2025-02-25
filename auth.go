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

		token := parts[1]
		resp, err := http.Get(cfg.AuthService + "/profile")
		if err != nil || resp.StatusCode != http.StatusOK {
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