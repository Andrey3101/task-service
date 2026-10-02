// Package auth — проверка access-токена ClerkAI (docs/API.md, раздел 3) и мидлварь Gin.
package auth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Issuer — ожидаемый iss.
const Issuer = "clerkai-auth"

const contextKey = "clerkai.user"

// User — текущий пользователь из access-токена.
type User struct {
	ID          uuid.UUID
	CompanyID   uuid.UUID
	CompanyType string
	Role        string
	Name        string
	Email       string
}

// IsAdmin — owner или admin.
func (u User) IsAdmin() bool { return u.Role == "owner" || u.Role == "admin" }

// ErrInvalidToken — токен не прошёл проверку.
var ErrInvalidToken = errors.New("invalid token")

// ParseAccessToken проверяет подпись HS256, iss, exp, typ=access и UUID в sub/company_id.
func ParseAccessToken(tokenString string, secret []byte) (User, error) {
	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(tokenString, claims,
		func(*jwt.Token) (interface{}, error) { return secret, nil },
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(Issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	)
	if err != nil {
		return User{}, ErrInvalidToken
	}
	if str(claims, "typ") != "access" {
		return User{}, ErrInvalidToken
	}
	if _, ok := claims["iat"]; !ok {
		return User{}, ErrInvalidToken
	}
	userID, err := uuid.Parse(str(claims, "sub"))
	if err != nil || userID == uuid.Nil {
		return User{}, ErrInvalidToken
	}
	companyID, err := uuid.Parse(str(claims, "company_id"))
	if err != nil || companyID == uuid.Nil {
		return User{}, ErrInvalidToken
	}
	u := User{
		ID:          userID,
		CompanyID:   companyID,
		CompanyType: str(claims, "company_type"),
		Role:        str(claims, "role"),
		Name:        str(claims, "name"),
		Email:       str(claims, "email"),
	}
	if u.CompanyType == "" {
		u.CompanyType = "self_employed"
	}
	if u.Role == "" {
		u.Role = "user"
	}
	return u, nil
}

func str(claims jwt.MapClaims, key string) string {
	s, _ := claims[key].(string)
	return s
}

// Middleware требует валидный Bearer-токен и кладёт User в контекст запроса.
func Middleware(secret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		scheme, token, ok := strings.Cut(strings.TrimSpace(c.GetHeader("Authorization")), " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
			unauthorized(c)
			return
		}
		user, err := ParseAccessToken(strings.TrimSpace(token), secret)
		if err != nil {
			unauthorized(c)
			return
		}
		c.Set(contextKey, user)
		c.Next()
	}
}

func unauthorized(c *gin.Context) {
	c.Header("WWW-Authenticate", "Bearer")
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"detail": "Не авторизован"})
}

// CurrentUser достаёт пользователя, положенного Middleware.
func CurrentUser(c *gin.Context) (User, bool) {
	v, ok := c.Get(contextKey)
	if !ok {
		return User{}, false
	}
	u, ok := v.(User)
	return u, ok
}
