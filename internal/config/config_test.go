package config

import (
	"log/slog"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{"JWT_SECRET": "0123456789abcdef0123456789abcdef"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "8009" || cfg.NotificationServiceURL != "http://notification-service:8004" ||
		cfg.DatabaseURL != "postgres://user:pass@postgres:5432/tasks_db?sslmode=disable" ||
		cfg.LogLevel != slog.LevelInfo || len(cfg.TrustedProxies) != len(DefaultTrustedProxies) || cfg.ServiceToken != "" {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"JWT_SECRET":               "0123456789abcdef0123456789abcdef",
		"PORT":                     "9000",
		"LOG_LEVEL":                "debug",
		"TRUSTED_PROXIES":          "10.1.0.0/16, 127.0.0.1",
		"NOTIFICATION_SERVICE_URL": "http://n:1/",
		"SERVICE_TOKEN":            " tok ",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "9000" || cfg.LogLevel != slog.LevelDebug || len(cfg.TrustedProxies) != 2 ||
		cfg.NotificationServiceURL != "http://n:1" || cfg.ServiceToken != "tok" {
		t.Errorf("unexpected cfg: %+v", cfg)
	}
}

func TestLoadErrors(t *testing.T) {
	for name, m := range map[string]map[string]string{
		"no secret":    {},
		"short secret": {"JWT_SECRET": "short"},
		"bad port":     {"JWT_SECRET": "0123456789abcdef0123456789abcdef", "PORT": "abc"},
		"bad level":    {"JWT_SECRET": "0123456789abcdef0123456789abcdef", "LOG_LEVEL": "loud"},
	} {
		if _, err := Load(env(m)); err == nil {
			t.Errorf("%s: ожидалась ошибка", name)
		}
	}
}
