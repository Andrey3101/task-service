package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	v1 "github.com/andre/task-service/api/v1"
	"github.com/andre/task-service/config"
	"github.com/andre/task-service/handlers"
	"github.com/andre/task-service/middleware"
	"github.com/andre/task-service/queue"
	"github.com/andre/task-service/services"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/gorilla/websocket"
	"github.com/streadway/amqp"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Константы для WebSocket
const (
	WebsocketReadDeadline  = 60 * time.Second
	WebsocketPingPeriod    = 10 * time.Second
	WebsocketWriteDeadline = 10 * time.Second
)

// App - структура для хранения зависимостей приложения
type App struct {
	DBConn               *sql.DB
	DB                   *gorm.DB
	Redis                *redis.Client
	RabbitMQ             *amqp.Connection
	RabbitMQChan         *amqp.Channel
	Router               *gin.Engine
	TaskService          *services.TaskService
	MeetingService       *services.MeetingService
	ImportantDateService *services.ImportantDateService
	IntegrationService   *services.IntegrationService
	ContactService       *services.ContactService
	NotificationService  *services.NotificationService
	CacheService         *services.CacheService
	QueueService         *services.QueueService
	QueueServiceOld      *queue.QueueService
	TaskHandler          *handlers.TaskHandler
	StartTime            time.Time
	Logger               *zap.Logger
}

// upgrader - конфигурация для WebSocket с улучшенной проверкой
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Здесь можно добавить более строгую проверку происхождения
		return true
	},
}

// setupDependencies инициализирует все зависимости приложения
func setupDependencies(cfg *config.Config, logger *zap.Logger) (*App, error) {
	// Подключение к базе данных
	dbConn, err := sql.Open("postgres", cfg.Database.GetDSN())
	if err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
		return nil, err
	}

	// Настройка пула соединений
	dbConn.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	dbConn.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	dbConn.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)

	// Проверка подключения к базе данных
	if err := dbConn.Ping(); err != nil {
		logger.Fatal("Failed to ping database", zap.Error(err))
		return nil, err
	}

	// Инициализация GORM
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: dbConn}), &gorm.Config{})
	if err != nil {
		logger.Fatal("Failed to initialize GORM", zap.Error(err))
		return nil, err
	}

	// Подключение к Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr:         cfg.Redis.GetRedisAddr(),
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		PoolSize:     cfg.Redis.PoolSize,
		MinIdleConns: cfg.Redis.MinIdleConns,
		MaxRetries:   cfg.Redis.MaxRetries,
		DialTimeout:  cfg.Redis.DialTimeout,
		ReadTimeout:  cfg.Redis.ReadTimeout,
		WriteTimeout: cfg.Redis.WriteTimeout,
	})

	// Проверка подключения к Redis
	ctx := context.Background()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Fatal("Failed to connect to Redis", zap.Error(err))
		return nil, err
	}

	// Подключение к RabbitMQ
	rabbitConn, err := amqp.Dial(cfg.RabbitMQ.URL)
	if err != nil {
		logger.Fatal("Failed to connect to RabbitMQ", zap.Error(err))
		return nil, err
	}

	rabbitCh, err := rabbitConn.Channel()
	if err != nil {
		logger.Fatal("Failed to open RabbitMQ channel", zap.Error(err))
		return nil, err
	}

	// Инициализация Gin
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.Logger())

	// Инициализация сервисов
	googleCalendar := services.NewGoogleCalendarService(cfg, redisClient)
	taskService, err := services.NewTaskService(db, redisClient, googleCalendar, cfg.RabbitMQ)
	if err != nil {
		logger.Fatal("Failed to initialize TaskService", zap.Error(err))
		return nil, err
	}

	// Инициализация новых сервисов
	meetingService := services.NewMeetingService(db)
	importantDateService := services.NewImportantDateService(db)
	integrationService := services.NewIntegrationService(db)
	contactService := services.NewContactService(db)
	notificationService := services.NewNotificationService(cfg, logger)
	cacheService := services.NewCacheService(redisClient, logger)
	queueService, err := services.NewQueueService(cfg, logger)
	if err != nil {
		logger.Fatal("Failed to initialize QueueService", zap.Error(err))
		return nil, err
	}

	queueServiceOld := queue.NewQueueService(cfg, taskService)
	taskHandler := handlers.NewTaskHandler(taskService, cfg)

	app := &App{
		DBConn:               dbConn, // Сохраняем *sql.DB для закрытия
		DB:                   db,
		Redis:                redisClient,
		RabbitMQ:             rabbitConn,
		RabbitMQChan:         rabbitCh,
		Router:               r,
		TaskService:          taskService,
		MeetingService:       meetingService,
		ImportantDateService: importantDateService,
		IntegrationService:   integrationService,
		ContactService:       contactService,
		NotificationService:  notificationService,
		CacheService:         cacheService,
		QueueService:         queueService,
		QueueServiceOld:      queueServiceOld,
		TaskHandler:          taskHandler,
		StartTime:            time.Now(),
		Logger:               logger,
	}

	return app, nil
}

// setupRoutes настраивает маршруты приложения
func setupRoutes(app *App) {
	// Middleware
	app.Router.Use(middleware.CORS())
	app.Router.Use(middleware.RateLimit())
	app.Router.Use(middleware.Logging(app.Logger))
	app.Router.Use(middleware.Security())

	// Swagger документация
	app.Router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// API версионирование
	v1.SetupRoutes(app.Router, app.TaskHandler)

	// WebSocket endpoint
	app.Router.GET("/ws", app.handleWebSocket)

	// Health check
	app.Router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "ok",
			"timestamp": time.Now().Unix(),
			"service":   "task-service",
			"version":   "1.0.0",
		})
	})

	// Metrics endpoint для Prometheus
	app.Router.GET("/metrics", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"service": "task-service",
			"metrics": map[string]interface{}{
				"uptime":  time.Since(app.StartTime).Seconds(),
				"version": "1.0.0",
			},
		})
	})
}

// handleWebSocket обрабатывает WebSocket соединения
func (app *App) handleWebSocket(c *gin.Context) {
	// Обновление HTTP соединения до WebSocket
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // В продакшене здесь должна быть проверка origin
		},
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		app.Logger.Error("Failed to upgrade connection to WebSocket", zap.Error(err))
		return
	}
	defer conn.Close()

	// Настройка WebSocket соединения
	conn.SetReadDeadline(time.Now().Add(WebsocketReadDeadline))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(WebsocketReadDeadline))
		return nil
	})

	// Канал для закрытия соединения
	done := make(chan struct{})

	// Горутина для чтения сообщений
	go func() {
		defer close(done)
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					app.Logger.Error("WebSocket read error", zap.Error(err))
				}
				break
			}

			// Обработка сообщения
			var wsMessage struct {
				Type string                 `json:"type"`
				Data map[string]interface{} `json:"data"`
			}

			if err := json.Unmarshal(message, &wsMessage); err != nil {
				app.Logger.Error("Failed to unmarshal WebSocket message", zap.Error(err))
				continue
			}

			// Обработка различных типов сообщений
			switch wsMessage.Type {
			case "ping":
				conn.WriteJSON(gin.H{"type": "pong", "timestamp": time.Now().Unix()})
			case "subscribe":
				// Подписка на обновления
				app.handleWebSocketSubscription(conn, wsMessage.Data)
			default:
				app.Logger.Warn("Unknown WebSocket message type", zap.String("type", wsMessage.Type))
			}
		}
	}()

	// Горутина для отправки ping сообщений
	go func() {
		ticker := time.NewTicker(WebsocketPingPeriod)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(WebsocketWriteDeadline)); err != nil {
					app.Logger.Error("Failed to send ping", zap.Error(err))
					return
				}
			case <-done:
				return
			}
		}
	}()

	// Ожидание закрытия соединения
	<-done
}

// handleWebSocketSubscription обрабатывает подписку на WebSocket обновления
func (app *App) handleWebSocketSubscription(conn *websocket.Conn, data map[string]interface{}) {
	// В реальности здесь будет логика подписки на обновления
	// Например, подписка на обновления задач, встреч и т.д.

	// Отправляем подтверждение подписки
	conn.WriteJSON(gin.H{
		"type": "subscription_confirmed",
		"data": gin.H{
			"message":   "Successfully subscribed to updates",
			"timestamp": time.Now().Unix(),
		},
	})
}

// startServer запускает HTTP сервер
func startServer(app *App, cfg *config.Config) error {
	server := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      app.Router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// Запуск сервера в горутине
	go func() {
		app.Logger.Info("Starting HTTP server", zap.String("port", cfg.Server.Port))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			app.Logger.Fatal("Failed to start server", zap.Error(err))
		}
	}()

	// Ожидание сигнала для graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	app.Logger.Info("Shutting down server...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		app.Logger.Error("Server forced to shutdown", zap.Error(err))
		return err
	}

	app.Logger.Info("Server exited")
	return nil
}

// cleanup выполняет очистку ресурсов
func (app *App) cleanup() {
	app.Logger.Info("Cleaning up resources...")

	// Закрытие соединений с базой данных
	if app.DBConn != nil {
		app.DBConn.Close()
	}

	// Закрытие соединения с Redis
	if app.Redis != nil {
		app.Redis.Close()
	}

	// Закрытие соединения с RabbitMQ
	if app.RabbitMQChan != nil {
		app.RabbitMQChan.Close()
	}
	if app.RabbitMQ != nil {
		app.RabbitMQ.Close()
	}

	// Закрытие сервиса очередей
	if app.QueueService != nil {
		app.QueueService.Close()
	}

	app.Logger.Info("Cleanup completed")
}

func main() {
	// Загрузка конфигурации
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("Failed to load config", err)
	}

	// Инициализация логгера
	logger, err := initLogger(cfg.Logging)
	if err != nil {
		log.Fatal("Failed to initialize logger", err)
	}
	defer logger.Sync()

	logger.Info("Starting Task Service", zap.String("version", "1.0.0"))

	// Инициализация зависимостей
	app, err := setupDependencies(cfg, logger)
	if err != nil {
		logger.Fatal("Failed to setup dependencies", err)
	}

	// Настройка маршрутов
	setupRoutes(app)

	// Запуск сервера
	if err := startServer(app, cfg); err != nil {
		logger.Fatal("Server error", zap.Error(err))
	}

	// Очистка ресурсов
	app.cleanup()
}

// initLogger инициализирует логгер
func initLogger(cfg config.LoggingConfig) (*zap.Logger, error) {
	var logger *zap.Logger
	var err error

	switch cfg.Format {
	case "json":
		logger, err = zap.NewProduction()
	case "console":
		logger, err = zap.NewDevelopment()
	default:
		logger, err = zap.NewProduction()
	}

	if err != nil {
		return nil, err
	}

	// Установка уровня логирования
	switch cfg.Level {
	case "debug":
		logger = logger.WithOptions(zap.IncreaseLevel(zap.DebugLevel))
	case "info":
		logger = logger.WithOptions(zap.IncreaseLevel(zap.InfoLevel))
	case "warn":
		logger = logger.WithOptions(zap.IncreaseLevel(zap.WarnLevel))
	case "error":
		logger = logger.WithOptions(zap.IncreaseLevel(zap.ErrorLevel))
	}

	return logger, nil
}
