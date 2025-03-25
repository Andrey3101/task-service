package services

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"time"

	"github.com/andre/task-service/config"
	"github.com/go-redis/redis/v8"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// AuthService - сервис аутентификации
type AuthService struct {
	Config        config.Config
	CurrentSecret string
	OldSecret     string
	Redis         *redis.Client
	DB            *gorm.DB
	Logger        *zap.Logger
}

// NewAuthService - конструктор AuthService
func NewAuthService(cfg config.Config, redisClient *redis.Client, db *gorm.DB) (*AuthService, error) {
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize logger: %v", err)
	}

	service := &AuthService{
		Config:        cfg,
		CurrentSecret: cfg.JWTSecret,
		Redis:         redisClient,
		DB:            db,
		Logger:        logger,
	}

	// Восстановление текущего секрета из Redis
	if secret, err := redisClient.Get(context.Background(), "jwt_current_secret").Result(); err == nil {
		service.CurrentSecret = service.decryptSecret(secret)
	}

	go service.rotateKeys()
	return service, nil
}

// encryptSecret - шифрование секрета перед сохранением в Redis
func (s *AuthService) encryptSecret(secret string) string {
	block, err := aes.NewCipher([]byte(s.Config.JWTEncryptionKey))
	if err != nil {
		s.Logger.Error("Failed to create AES cipher", zap.Error(err))
		return secret // fallback без шифрования
	}

	plaintext := []byte(secret)
	ciphertext := make([]byte, aes.BlockSize+len(plaintext))
	iv := ciphertext[:aes.BlockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		s.Logger.Error("Failed to generate IV", zap.Error(err))
		return secret
	}

	stream := cipher.NewCFBEncrypter(block, iv)
	stream.XORKeyStream(ciphertext[aes.BlockSize:], plaintext)
	return base64.StdEncoding.EncodeToString(ciphertext)
}

// decryptSecret - расшифровка секрета из Redis
func (s *AuthService) decryptSecret(encoded string) string {
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		s.Logger.Error("Failed to decode secret", zap.Error(err))
		return s.Config.JWTSecret // fallback на дефолтный секрет
	}

	block, err := aes.NewCipher([]byte(s.Config.JWTEncryptionKey))
	if err != nil {
		s.Logger.Error("Failed to create AES cipher", zap.Error(err))
		return s.Config.JWTSecret
	}

	if len(ciphertext) < aes.BlockSize {
		s.Logger.Error("Ciphertext too short")
		return s.Config.JWTSecret
	}

	iv := ciphertext[:aes.BlockSize]
	ciphertext = ciphertext[aes.BlockSize:]
	stream := cipher.NewCFBDecrypter(block, iv)
	stream.XORKeyStream(ciphertext, ciphertext)
	return string(ciphertext)
}

// rotateKeys - ротация JWT ключей
func (s *AuthService) rotateKeys() {
	ticker := time.NewTicker(time.Duration(s.Config.JWTRotationInterval) * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.OldSecret = s.CurrentSecret
		s.CurrentSecret = fmt.Sprintf("%s-%d", s.Config.JWTSecret, time.Now().Unix())
		s.Logger.Info("JWT Secret rotated", zap.String("new_secret", s.CurrentSecret))

		// Сохранение зашифрованного секрета в Redis
		encryptedCurrent := s.encryptSecret(s.CurrentSecret)
		s.Redis.Set(context.Background(), "jwt_current_secret", encryptedCurrent, 0)
		if s.OldSecret != "" {
			encryptedOld := s.encryptSecret(s.OldSecret)
			s.Redis.Set(context.Background(), "jwt_old_secret", encryptedOld, time.Duration(s.Config.JWTRotationInterval)*time.Second)
		}
	}
}

// Claims - структура для JWT claims
type Claims struct {
	UserID    string    `json:"sub"`
	Role      string    `json:"role"`
	CompanyID uuid.UUID `json:"company_id"`
	jwt.RegisteredClaims
}

// GenerateToken - генерация JWT токена
func (s *AuthService) GenerateToken(userID string, role string, companyID uuid.UUID) (string, error) {
	validRoles := []string{"user", "manager", "admin", "super_admin"}
	isValidRole := false
	for _, validRole := range validRoles {
		if role == validRole {
			isValidRole = true
			break
		}
	}
	if !isValidRole {
		return "", fmt.Errorf("invalid role: %s", role)
	}

	claims := Claims{
		UserID:    userID,
		Role:      role,
		CompanyID: companyID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ID:        uuid.New().String(), // Уникальный идентификатор токена
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString([]byte(s.CurrentSecret))
	if err != nil {
		s.Logger.Error("Failed to sign token", zap.Error(err))
		return "", err
	}

	// Кэширование роли в Redis
	s.Redis.Set(context.Background(), "role:"+userID, role, s.Config.RoleCacheTTL)
	return signedToken, nil
}

// ValidateToken - валидация JWT токена
func (s *AuthService) ValidateToken(tokenString string) (*Claims, error) {
	// Проверка blacklist
	if exists, err := s.Redis.Exists(context.Background(), "blacklist:"+tokenString).Result(); err == nil && exists > 0 {
		return nil, fmt.Errorf("token is blacklisted")
	}

	// Попытка валидации с текущим секретом
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(s.CurrentSecret), nil
	})
	if err == nil && token.Valid {
		if claims, ok := token.Claims.(*Claims); ok {
			if claims.ExpiresAt.Before(time.Now()) {
				return nil, fmt.Errorf("token expired")
			}
			s.Redis.Set(context.Background(), "role:"+claims.UserID, claims.Role, s.Config.RoleCacheTTL)
			return claims, nil
		}
	}

	// Попытка валидации со старым секретом
	if s.OldSecret != "" {
		token, err = jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
			return []byte(s.OldSecret), nil
		})
		if err == nil && token.Valid {
			if claims, ok := token.Claims.(*Claims); ok {
				if claims.ExpiresAt.Before(time.Now()) {
					return nil, fmt.Errorf("token expired")
				}
				s.Redis.Set(context.Background(), "role:"+claims.UserID, claims.Role, s.Config.RoleCacheTTL)
				return claims, nil
			}
		}
	}

	return nil, jwt.ErrSignatureInvalid
}

// BlacklistToken - добавление токена в blacklist
func (s *AuthService) BlacklistToken(tokenString string) error {
	err := s.Redis.Set(context.Background(), "blacklist:"+tokenString, "true", s.Config.BlacklistTTL).Err()
	if err != nil {
		s.Logger.Error("Failed to blacklist token", zap.Error(err))
	}
	return err
}

// GetRole - получение роли пользователя
func (s *AuthService) GetRole(userID string) (string, bool) {
	// Сначала проверка в Redis
	if role, err := s.Redis.Get(context.Background(), "role:"+userID).Result(); err == nil {
		return role, true
	}

	// Если в Redis нет, запрос в БД
	var role string
	err := s.DB.WithContext(context.Background()).Table("users").Where("user_id = ?", userID).Select("role").Scan(&role).Error
	if err != nil {
		s.Logger.Warn("Failed to fetch role from DB", zap.String("user_id", userID), zap.Error(err))
		return "", false
	}

	// Кэширование в Redis
	s.Redis.Set(context.Background(), "role:"+userID, role, s.Config.RoleCacheTTL)
	return role, true
}

// Profile - получение профиля пользователя по токену
func (s *AuthService) Profile(ctx context.Context, tokenString string) (map[string]interface{}, error) {
	claims, err := s.ValidateToken(tokenString)
	if err != nil {
		return nil, fmt.Errorf("invalid token: %v", err)
	}
	return map[string]interface{}{
		"user_id":    claims.UserID,
		"role":       claims.Role,
		"company_id": claims.CompanyID,
	}, nil
}

// CreateUser - создание нового пользователя
func (s *AuthService) CreateUser(ctx context.Context, creatorID, newUserID, role string) error {
	var creator struct {
		Role      string    `gorm:"column:role"`
		CompanyID uuid.UUID `gorm:"column:company_id"`
	}
	if err := s.DB.WithContext(ctx).Table("users").Where("user_id = ?", creatorID).First(&creator).Error; err != nil {
		return fmt.Errorf("creator not found: %v", err)
	}

	if creator.Role != "manager" && creator.Role != "admin" && creator.Role != "super_admin" {
		return fmt.Errorf("only managers, admins, and super_admins can create users")
	}
	if role != "user" && creator.Role != "admin" && creator.Role != "super_admin" {
		return fmt.Errorf("managers can only create users with role 'user'")
	}

	newUser := struct {
		UserID    string    `gorm:"column:user_id"`
		Role      string    `gorm:"column:role"`
		CompanyID uuid.UUID `gorm:"column:company_id"`
	}{UserID: newUserID, Role: role, CompanyID: creator.CompanyID}

	err := s.DB.WithContext(ctx).Table("users").Create(&newUser).Error
	if err == nil {
		s.Redis.Set(ctx, "role:"+newUserID, role, s.Config.RoleCacheTTL)
	}
	return err
}

// UpdateUserRole - обновление роли пользователя
func (s *AuthService) UpdateUserRole(ctx context.Context, updaterID, targetUserID, newRole string) error {
	var updater struct {
		Role string `gorm:"column:role"`
	}
	if err := s.DB.WithContext(ctx).Table("users").Where("user_id = ?", updaterID).First(&updater).Error; err != nil {
		return fmt.Errorf("updater not found: %v", err)
	}

	if updater.Role != "manager" && updater.Role != "admin" && updater.Role != "super_admin" {
		return fmt.Errorf("only managers, admins, and super_admins can update roles")
	}
	if newRole != "user" && updater.Role != "admin" && updater.Role != "super_admin" {
		return fmt.Errorf("managers can only set role to 'user'")
	}

	err := s.DB.WithContext(ctx).Exec("UPDATE users SET role = ? WHERE user_id = ?", newRole, targetUserID).Error
	if err == nil {
		s.Redis.Set(ctx, "role:"+targetUserID, newRole, s.Config.RoleCacheTTL)
	}
	return err
}

// Close - закрытие ресурсов сервиса
func (s *AuthService) Close() {
	s.Logger.Sync()
}
