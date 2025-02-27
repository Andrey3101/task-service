package services

import (
	"context"
	"fmt"
	"log"
	"sync"
	"tasks-service/config"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type AuthService struct {
	Config        config.Config
	RoleCache     sync.Map
	CurrentSecret string
	OldSecret     string
	AuthDBShards  map[int]*gorm.DB // Добавлено для управления пользователями
}

func NewAuthService(cfg config.Config) *AuthService {
	authDBShards := make(map[int]*gorm.DB)
	for i := 0; i < 2; i++ {
		dsn := fmt.Sprintf("postgres://user:password@localhost:5432/tasks?sslmode=disable&search_path=auth_shard_%d", i)
		shardDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
		if err != nil {
			log.Fatalf("Failed to connect to auth_shard_%d: %v", i, err)
		}
		authDBShards[i] = shardDB
	}
	service := &AuthService{
		Config:        cfg,
		CurrentSecret: cfg.JWTSecret,
		AuthDBShards:  authDBShards,
	}
	go service.rotateKeys()
	return service
}

func (s *AuthService) rotateKeys() {
	ticker := time.NewTicker(time.Duration(s.Config.JWTRotationInterval) * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.OldSecret = s.CurrentSecret
		s.CurrentSecret = fmt.Sprintf("%s-%d", s.Config.JWTSecret, time.Now().Unix())
		log.Printf("JWT Secret rotated")
	}
}

type Claims struct {
	UserID string `json:"sub"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

func (s *AuthService) GenerateToken(userID string, role string) (string, error) {
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
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString([]byte(s.CurrentSecret))
	if err == nil {
		s.RoleCache.Store(userID, role)
	}
	return signedToken, err
}

func (s *AuthService) ValidateToken(tokenString string) (*Claims, error) {
	if _, err := s.Redis.Get(context.Background(), "blacklist:"+tokenString).Result(); err == nil {
		return nil, fmt.Errorf("token is blacklisted")
	}
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(s.CurrentSecret), nil
	})
	if err == nil && token.Valid {
		if claims, ok := token.Claims.(*Claims); ok {
			s.RoleCache.Store(claims.UserID, claims.Role)
			return claims, nil
		}
	}

	if s.OldSecret != "" {
		token, err = jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
			return []byte(s.OldSecret), nil
		})
		if err == nil && token.Valid {
			if claims, ok := token.Claims.(*Claims); ok {
				s.RoleCache.Store(claims.UserID, claims.Role)
				return claims, nil
			}
		}
	}

	return nil, jwt.ErrSignatureInvalid
}

func (s *AuthService) GetRole(userID string) (string, bool) {
	if role, ok := s.RoleCache.Load(userID); ok {
		return role.(string), true
	}
	return "", false
}

func (s *AuthService) CreateUser(ctx context.Context, creatorID, newUserID, role string) error {
	creatorShard := s.determineUserShard(ctx, creatorID)
	if creatorShard == -1 {
		return fmt.Errorf("creator not found")
	}
	var creatorRole string
	s.AuthDBShards[creatorShard].Raw("SELECT role FROM users WHERE user_id = ?", creatorID).Scan(&creatorRole)
	if creatorRole != "manager" && creatorRole != "admin" && creatorRole != "super_admin" {
		return fmt.Errorf("only managers, admins, and super_admins can create users")
	}
	if role != "user" && creatorRole != "admin" && creatorRole != "super_admin" {
		return fmt.Errorf("managers can only create users with role 'user'")
	}
	var companyID uuid.UUID
	s.AuthDBShards[creatorShard].Raw("SELECT company_id FROM users WHERE user_id = ?", creatorID).Scan(&companyID)
	newUser := struct {
		UserID    string    `gorm:"column:user_id"`
		Role      string    `gorm:"column:role"`
		CompanyID uuid.UUID `gorm:"column:company_id"`
	}{UserID: newUserID, Role: role, CompanyID: companyID}
	return s.AuthDBShards[creatorShard].WithContext(ctx).Table("users").Create(&newUser).Error
}

func (s *AuthService) UpdateUserRole(ctx context.Context, updaterID, targetUserID, newRole string) error {
	updaterShard := s.determineUserShard(ctx, updaterID)
	if updaterShard == -1 {
		return fmt.Errorf("updater not found")
	}
	var updaterRole string
	s.AuthDBShards[updaterShard].Raw("SELECT role FROM users WHERE user_id = ?", updaterID).Scan(&updaterRole)
	if updaterRole != "manager" && updaterRole != "admin" && updaterRole != "super_admin" {
		return fmt.Errorf("only managers, admins, and super_admins can update roles")
	}
	if newRole != "user" && updaterRole != "admin" && updaterRole != "super_admin" {
		return fmt.Errorf("managers can only set role to 'user'")
	}
	targetShard := s.determineUserShard(ctx, targetUserID)
	if targetShard == -1 {
		return fmt.Errorf("target user not found")
	}
	return s.AuthDBShards[targetShard].WithContext(ctx).Exec(
		"UPDATE users SET role = ? WHERE user_id = ?", newRole, targetUserID).Error
}

func (s *AuthService) determineUserShard(ctx context.Context, userID string) int {
	for shard, db := range s.AuthDBShards {
		var count int64
		db.WithContext(ctx).Raw("SELECT COUNT(*) FROM users WHERE user_id = ?", userID).Scan(&count)
		if count > 0 {
			return shard
		}
	}
	return -1
}
