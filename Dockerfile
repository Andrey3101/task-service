# Этап 1: Сборка Go-приложения
FROM golang:1.23 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
RUN go install github.com/swaggo/swag/cmd/swag@latest
COPY . .
RUN swag init -g main.go  # Генерация Swagger-документации
RUN CGO_ENABLED=0 GOOS=linux go build -o task-service ./main.go

# Этап 2: Создание минимального контейнера
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/task-service .
EXPOSE 8082
CMD ["./task-service"]