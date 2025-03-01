package config

import "os"

type Config struct {
	JWTSecret           string
	JWTRotationInterval int
	DBConn              string
	RabbitMQ            string
	Port                string
	RedisAddr           string
	JWTSecretOld        string
	Language            string
	GoogleClientID      string
	GoogleClientSecret  string
	GoogleRedirectURI   string
}

func LoadConfig() Config {
	return Config{
		JWTSecret:           getEnv("JWT_SECRET", "your_initial_secret_key"),
		JWTRotationInterval: 86400,
		DBConn:              getEnv("DB_CONN", "postgres://postgres:mysecretpassword@localhost:5432/tasks?sslmode=disable"),
		RabbitMQ:            getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		Port:                getEnv("PORT", "8082"),
		RedisAddr:           getEnv("REDIS_ADDR", "localhost:6379"),
		JWTSecretOld:        getEnv("JWT_SECRET_OLD", ""),
		Language:            getEnv("LANGUAGE", "en"),
		GoogleClientID:      getEnv("GOOGLE_CLIENT_ID", "your-google-client-id"),
		GoogleClientSecret:  getEnv("GOOGLE_CLIENT_SECRET", "your-google-client-secret"),
		GoogleRedirectURI:   getEnv("GOOGLE_REDIRECT_URI", "http://localhost:8082/google/callback"),
	}
}

// getEnv получает значение переменной окружения или возвращает дефолтное значение
func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
