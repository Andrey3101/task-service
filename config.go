package config

import "os"

type Config struct {
	JWTSecret           string // Текущий секретный ключ для JWT
	JWTRotationInterval int    // Интервал ротации ключа в секундах (по умолчанию 86400 = 1 день)
	DBConn              string // Строка подключения к PostgreSQL
	RabbitMQ            string // URL для RabbitMQ
	Port                string // Порт сервиса
	RedisAddr           string // Адрес Redis
	JWTSecretOld        string // Старый ключ для переходного периода
	Language            string // Язык интерфейса ("en" или "ru")
}

func LoadConfig() Config {
	return Config{
		JWTSecret:           os.Getenv("JWT_SECRET", "your_initial_secret_key"),
		JWTRotationInterval: 86400,
		DBConn:              os.Getenv("DB_CONN", "postgres://user:password@localhost:5432/tasks?sslmode=disable"),
		RabbitMQ:            os.Getenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		Port:                os.Getenv("PORT", "8082"),
		RedisAddr:           os.Getenv("REDIS_ADDR", "localhost:6379"),
		JWTSecretOld:        os.Getenv("JWT_SECRET_OLD", ""),
		Language:            os.Getenv("LANGUAGE", "en"),
	}
}
