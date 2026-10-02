package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var secret = []byte("test-secret-test-secret-test-secret!")

var (
	userID    = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	companyID = uuid.MustParse("22222222-2222-2222-2222-222222222222")
)

func claims(mod func(jwt.MapClaims)) jwt.MapClaims {
	now := time.Now()
	c := jwt.MapClaims{
		"sub":          userID.String(),
		"company_id":   companyID.String(),
		"company_type": "ip",
		"role":         "admin",
		"email":        "user@example.com",
		"name":         "Иван Петров",
		"typ":          "access",
		"iss":          Issuer,
		"iat":          now.Unix(),
		"exp":          now.Add(30 * time.Minute).Unix(),
		"jti":          uuid.NewString(),
	}
	if mod != nil {
		mod(c)
	}
	return c
}

func sign(t *testing.T, method jwt.SigningMethod, c jwt.MapClaims, key []byte) string {
	t.Helper()
	s, err := jwt.NewWithClaims(method, c).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Middleware(secret))
	r.GET("/me", func(c *gin.Context) {
		u, ok := CurrentUser(c)
		if !ok {
			c.Status(http.StatusTeapot)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"id": u.ID.String(), "company_id": u.CompanyID.String(), "company_type": u.CompanyType,
			"role": u.Role, "name": u.Name, "email": u.Email, "admin": u.IsAdmin(),
		})
	})
	return r
}

func do(r http.Handler, header string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestMiddlewareValidToken(t *testing.T) {
	w := do(newRouter(), "Bearer "+sign(t, jwt.SigningMethodHS256, claims(nil), secret))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["id"] != userID.String() || got["company_id"] != companyID.String() || got["company_type"] != "ip" ||
		got["role"] != "admin" || got["name"] != "Иван Петров" || got["email"] != "user@example.com" || got["admin"] != true {
		t.Errorf("unexpected user: %v", got)
	}
}

func TestMiddlewareLowercaseScheme(t *testing.T) {
	if w := do(newRouter(), "bearer "+sign(t, jwt.SigningMethodHS256, claims(nil), secret)); w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
}

func TestMiddlewareDefaults(t *testing.T) {
	tok := sign(t, jwt.SigningMethodHS256, claims(func(c jwt.MapClaims) {
		delete(c, "role")
		delete(c, "company_type")
	}), secret)
	u, err := ParseAccessToken(tok, secret)
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != "user" || u.CompanyType != "self_employed" || u.IsAdmin() {
		t.Errorf("defaults: %+v", u)
	}
}

func TestMiddlewareRejects(t *testing.T) {
	cases := map[string]string{
		"no header":     "",
		"not bearer":    "Basic abc",
		"empty token":   "Bearer ",
		"garbage":       "Bearer not.a.jwt",
		"wrong secret":  "Bearer " + sign(t, jwt.SigningMethodHS256, claims(nil), []byte("another-secret-another-secret-123")),
		"hs512":         "Bearer " + sign(t, jwt.SigningMethodHS512, claims(nil), secret),
		"expired":       "Bearer " + sign(t, jwt.SigningMethodHS256, claims(func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Minute).Unix() }), secret),
		"no exp":        "Bearer " + sign(t, jwt.SigningMethodHS256, claims(func(c jwt.MapClaims) { delete(c, "exp") }), secret),
		"no iat":        "Bearer " + sign(t, jwt.SigningMethodHS256, claims(func(c jwt.MapClaims) { delete(c, "iat") }), secret),
		"refresh":       "Bearer " + sign(t, jwt.SigningMethodHS256, claims(func(c jwt.MapClaims) { c["typ"] = "refresh" }), secret),
		"wrong issuer":  "Bearer " + sign(t, jwt.SigningMethodHS256, claims(func(c jwt.MapClaims) { c["iss"] = "evil" }), secret),
		"bad sub":       "Bearer " + sign(t, jwt.SigningMethodHS256, claims(func(c jwt.MapClaims) { c["sub"] = "42" }), secret),
		"no company":    "Bearer " + sign(t, jwt.SigningMethodHS256, claims(func(c jwt.MapClaims) { delete(c, "company_id") }), secret),
		"nil company":   "Bearer " + sign(t, jwt.SigningMethodHS256, claims(func(c jwt.MapClaims) { c["company_id"] = uuid.Nil.String() }), secret),
		"alg none":      "Bearer eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0." + jwt.NewWithClaims(jwt.SigningMethodHS256, claims(nil)).EncodeSegment([]byte(`{"sub":"x"}`)) + ".",
		"future iat":    "Bearer " + sign(t, jwt.SigningMethodHS256, claims(func(c jwt.MapClaims) { c["iat"] = time.Now().Add(time.Hour).Unix() }), secret),
		"numeric typ":   "Bearer " + sign(t, jwt.SigningMethodHS256, claims(func(c jwt.MapClaims) { c["typ"] = 1 }), secret),
		"no typ":        "Bearer " + sign(t, jwt.SigningMethodHS256, claims(func(c jwt.MapClaims) { delete(c, "typ") }), secret),
		"string exp":    "Bearer " + sign(t, jwt.SigningMethodHS256, claims(func(c jwt.MapClaims) { c["exp"] = "tomorrow" }), secret),
		"extra spaces":  "Bearer  x y",
		"only scheme":   "Bearer",
		"unknown chars": "Bearer \x7f",
	}
	r := newRouter()
	for name, header := range cases {
		w := do(r, header)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: status %d", name, w.Code)
			continue
		}
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["detail"] != "Не авторизован" {
			t.Errorf("%s: body %s", name, w.Body.String())
		}
		if w.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Errorf("%s: нет WWW-Authenticate", name)
		}
	}
}
