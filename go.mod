module tasks-service

go 1.21

require (
    github.com/gin-gonic/gin v1.10.0
    gorm.io/driver/postgres v1.5.9
    gorm.io/gorm v1.25.12
    github.com/streadway/amqp v1.0.0
    github.com/google/uuid v1.6.0
    github.com/go-redis/redis/v8 v8.11.5 // Для Redis
    github.com/golang-jwt/jwt/v5 v5.2.1  // Для JWT
)