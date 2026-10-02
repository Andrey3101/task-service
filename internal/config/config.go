// Package config читает настройки сервиса из переменных окружения.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

// DefaultTrustedProxies — частные сети (docker/compose), из которых принимаются X-Forwarded-*.
var DefaultTrustedProxies = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"127.0.0.0/8",
	"::1/128",
	"fc00::/7",
}

// Config — настройки task-service.
type Config struct {
	Port                   string
	DatabaseURL            string
	JWTSecret              string
	ServiceToken           string
	NotificationServiceURL string
	LogLevel               slog.Level
	TrustedProxies         []string
}

// Load собирает конфигурацию; getenv обычно os.Getenv.
func Load(getenv func(string) string) (Config, error) {
	get := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}

	cfg := Config{
		Port:                   get("PORT", "8009"),
		DatabaseURL:            get("DATABASE_URL", "postgres://user:pass@postgres:5432/tasks_db?sslmode=disable"),
		JWTSecret:              getenv("JWT_SECRET"),
		ServiceToken:           strings.TrimSpace(getenv("SERVICE_TOKEN")),
		NotificationServiceURL: strings.TrimRight(get("NOTIFICATION_SERVICE_URL", "http://notification-service:8004"), "/"),
		TrustedProxies:         DefaultTrustedProxies,
	}

	if len(cfg.JWTSecret) < 32 {
		return Config{}, errors.New("JWT_SECRET обязателен и должен быть не короче 32 символов")
	}
	if p, err := strconv.Atoi(cfg.Port); err != nil || p < 1 || p > 65535 {
		return Config{}, fmt.Errorf("некорректный PORT: %q", cfg.Port)
	}

	level, err := ParseLogLevel(get("LOG_LEVEL", "INFO"))
	if err != nil {
		return Config{}, err
	}
	cfg.LogLevel = level

	if raw := strings.TrimSpace(getenv("TRUSTED_PROXIES")); raw != "" {
		cfg.TrustedProxies = SplitList(raw)
	}
	return cfg, nil
}

// ParseLogLevel понимает DEBUG/INFO/WARN(ING)/ERROR в любом регистре.
func ParseLogLevel(s string) (slog.Level, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DEBUG":
		return slog.LevelDebug, nil
	case "", "INFO":
		return slog.LevelInfo, nil
	case "WARN", "WARNING":
		return slog.LevelWarn, nil
	case "ERROR", "CRITICAL":
		return slog.LevelError, nil
	}
	return slog.LevelInfo, fmt.Errorf("некорректный LOG_LEVEL: %q", s)
}

// SplitList разбивает строку "a, b,,c" в []string{"a","b","c"}.
func SplitList(raw string) []string {
	out := make([]string, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
