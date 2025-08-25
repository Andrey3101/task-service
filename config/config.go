package config

import (
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config основная конфигурация приложения
type Config struct {
	Server              ServerConfig
	Database            DatabaseConfig
	Redis               RedisConfig
	RabbitMQ            RabbitMQConfig
	Auth                AuthConfig
	JWT                 JWTConfig
	Logging             LoggingConfig
	Language            string
	NotificationService NotificationServiceConfig
}

// ServerConfig конфигурация сервера
type ServerConfig struct {
	Port         string
	Host         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// DatabaseConfig конфигурация базы данных
type DatabaseConfig struct {
	Host            string
	Port            string
	User            string
	Password        string
	DBName          string
	SSLMode         string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// RedisConfig конфигурация Redis
type RedisConfig struct {
	Host     string
	Port     string
	Password string
	DB       int
}

// RabbitMQConfig конфигурация RabbitMQ
type RabbitMQConfig struct {
	URL        string
	MaxRetries int
}

// NotificationServiceConfig конфигурация сервиса уведомлений
type NotificationServiceConfig struct {
	URL     string
	APIKey  string
	Timeout time.Duration
}

// LoggingConfig конфигурация логирования
type LoggingConfig struct {
	Level      string
	Format     string
	OutputPath string
}

// AuthConfig конфигурация для auth-service
type AuthConfig struct {
	ServiceURL string
	Timeout    time.Duration
}

// JWTConfig конфигурация для JWT
type JWTConfig struct {
	Secret           string
	EncryptionKey    string
	RotationInterval int
	RoleCacheTTL     time.Duration
	BlacklistTTL     time.Duration
}

// Load загружает конфигурацию из переменных окружения
func Load() (*Config, error) {
	// Загрузка .env файла если существует
	if err := godotenv.Load(); err != nil {
		// Игнорируем ошибку если файл не найден
	}

	cfg := &Config{
		Server: ServerConfig{
			Port:         getStringEnv("SERVER_PORT", "8009"),
			Host:         getStringEnv("SERVER_HOST", "0.0.0.0"),
			ReadTimeout:  getDurationEnv("SERVER_READ_TIMEOUT", 30*time.Second),
			WriteTimeout: getDurationEnv("SERVER_WRITE_TIMEOUT", 30*time.Second),
		},
		Database: DatabaseConfig{
			Host:            getStringEnv("DB_HOST", "localhost"),
			Port:            getStringEnv("DB_PORT", "5432"),
			User:            getStringEnv("DB_USER", "postgres"),
			Password:        getStringEnv("DB_PASSWORD", "password"),
			DBName:          getStringEnv("DB_NAME", "task_service"),
			SSLMode:         getStringEnv("DB_SSLMODE", "disable"),
			MaxOpenConns:    getIntEnv("DB_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    getIntEnv("DB_MAX_IDLE_CONNS", 5),
			ConnMaxLifetime: getDurationEnv("DB_CONN_MAX_LIFETIME", 5*time.Minute),
		},
		Redis: RedisConfig{
			Host:     getStringEnv("REDIS_HOST", "localhost"),
			Port:     getStringEnv("REDIS_PORT", "6379"),
			Password: getStringEnv("REDIS_PASSWORD", ""),
			DB:       getIntEnv("REDIS_DB", 0),
		},
		RabbitMQ: RabbitMQConfig{
			URL:        getStringEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
			MaxRetries: getIntEnv("RABBITMQ_MAX_RETRIES", 3),
		},
		Auth: AuthConfig{
			ServiceURL: getStringEnv("AUTH_SERVICE_URL", "http://auth-service:8000"),
			Timeout:    getDurationEnv("AUTH_TIMEOUT", 10*time.Second),
		},
		JWT: JWTConfig{
			Secret:           getStringEnv("JWT_SECRET", "your-super-secret-jwt-key-change-in-production"),
			EncryptionKey:    getStringEnv("JWT_ENCRYPTION_KEY", "your-encryption-key-32-chars"),
			RotationInterval: getIntEnv("JWT_ROTATION_INTERVAL", 3600),
			RoleCacheTTL:     getDurationEnv("JWT_ROLE_CACHE_TL", 24*time.Hour),
			BlacklistTTL:     getDurationEnv("JWT_BLACKLIST_TTL", 7*24*time.Hour),
		},
		Logging: LoggingConfig{
			Level: getStringEnv("LOG_LEVEL", "info"),
		},
		Language: getStringEnv("LANGUAGE", "en"),
		NotificationService: NotificationServiceConfig{
			URL:     getStringEnv("NOTIFICATION_SERVICE_URL", "http://notification-service:8000"),
			APIKey:  getStringEnv("NOTIFICATION_SERVICE_API_KEY", "default-api-key"),
			Timeout: getDurationEnv("NOTIFICATION_SERVICE_TIMEOUT", 30*time.Second),
		},
	}

	return cfg, nil
}

// GetDSN возвращает строку подключения к базе данных
func (c *DatabaseConfig) GetDSN() string {
	return "host=" + c.Host +
		" port=" + c.Port +
		" user=" + c.User +
		" password=" + c.Password +
		" dbname=" + c.DBName +
		" sslmode=" + c.SSLMode
}

// GetRedisAddr возвращает адрес Redis
func (c *RedisConfig) GetAddr() string {
	return c.Host + ":" + c.Port
}

// Вспомогательные функции для получения переменных окружения
func getStringEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getIntEnv(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getDurationEnv(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}
