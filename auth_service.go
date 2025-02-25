package services

import (
	"fmt"
	"log"
	"sync"
	"tasks-service/config"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type AuthService struct {
	Config        config.Config
	RoleCache     sync.Map
	CurrentSecret string
	OldSecret     string
}

func NewAuthService(cfg config.Config) *AuthService {
	service := &AuthService{
		Config:        cfg,
		CurrentSecret: cfg.JWTSecret,
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
