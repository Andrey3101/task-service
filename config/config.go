package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	JWTSecret           string        // Секрет для JWT
	JWTRotationInterval int           // Интервал ротации JWT секретов (в секундах)
	JWTEncryptionKey    string        // Ключ шифрования для JWT секретов
	BlacklistTTL        time.Duration // Время жизни токенов в blacklist
	RoleCacheTTL        time.Duration // Время жизни кэша ролей в Redis
	DBConn              string        // Строка подключения к базе данных
	RabbitMQ            string        // URL для RabbitMQ
	Port                string        // Порт сервера
	RedisAddr           string        // Адрес Redis
	JWTSecretOld        string        // Старый секрет JWT (опционально)
	Language            string        // Язык приложения
	GoogleClientID      string        // Google Client ID
	GoogleClientSecret  string        // Google Client Secret
	GoogleRedirectURI   string        // Google Redirect URI
	AuthService         string        // URL сервиса аутентификации
}

func LoadConfig() Config {
	return Config{
		JWTSecret:           getEnv("JWT_SECRET", "your_initial_secret_key"),
		JWTRotationInterval: getEnvAsInt("JWT_ROTATION_INTERVAL", 86400),                                              // 24 часа в секундах
		JWTEncryptionKey:    getEnv("JWT_ENCRYPTION_KEY", "32-byte-long-secret-key-for-aes!"),                         // Должен быть 32 байта для AES-256
		BlacklistTTL:        time.Duration(getEnvAsInt("BLACKLIST_TTL", int(24*time.Hour/time.Second))) * time.Second, // 24 часа
		RoleCacheTTL:        time.Duration(getEnvAsInt("ROLE_CACHE_TTL", int(1*time.Hour/time.Second))) * time.Second, // 1 час
		DBConn:              getEnv("DB_CONN", "postgresql://postgres:mysecretpassword@host.docker.internal:5432/tasks"),
		RabbitMQ:            getEnv("RABBITMQ_URL", "amqp://guest:guest@rabbitmq:5672/"),
		Port:                getEnv("PORT", "8082"),
		RedisAddr:           getEnv("REDIS_ADDR", "redis:6379"),
		JWTSecretOld:        getEnv("JWT_SECRET_OLD", ""),
		Language:            getEnv("LANGUAGE", "en"),
		GoogleClientID:      getEnv("GOOGLE_CLIENT_ID", "your-google-client-id"),
		GoogleClientSecret:  getEnv("GOOGLE_CLIENT_SECRET", "your-google-client-secret"),
		GoogleRedirectURI:   getEnv("GOOGLE_REDIRECT_URI", "http://localhost:8082/google/callback"),
		AuthService:         getEnv("AUTH_SERVICE", "http://localhost:8082"),
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
