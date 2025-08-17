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
	Config               *config.Config
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

// healthCheck проверка состояния сервиса
func (app *App) healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"timestamp": time.Now().Unix(),
		"service":   "task-service",
		"version":   "1.0.0",
		"uptime":    time.Since(app.StartTime).Seconds(),
	})
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
	app.TaskService = services.NewTaskService(app.DB, app.Redis, app.RabbitMQChan, app.Logger)
	app.MeetingService = services.NewMeetingService(app.DB, app.Redis, app.Logger)
	app.ImportantDateService = services.NewImportantDateService(app.DB, app.Redis, app.Logger)
	app.IntegrationService = services.NewIntegrationService(app.DB, app.Redis, app.Logger)
	app.ContactService = services.NewContactService(app.DB, app.Redis, app.Logger)
	app.NotificationService = services.NewNotificationService(app.Redis, app.RabbitMQChan, app.Logger)
	app.CacheService = services.NewCacheService(app.Redis, app.Logger)
	app.QueueService = services.NewQueueService(app.RabbitMQChan, app.Logger)
	app.QueueServiceOld = queue.NewQueueService(app.RabbitMQChan, app.Logger)
	app.GoogleCalendarService = services.NewGoogleCalendarService(app.DB, app.Logger)

	taskHandler := handlers.NewTaskHandler(app.TaskService, cfg)

	app := &App{
		Config:               cfg,
		DBConn:               dbConn, // Сохраняем *sql.DB для закрытия
		DB:                   db,
		Redis:                redisClient,
		RabbitMQ:             rabbitConn,
		RabbitMQChan:         rabbitCh,
		Router:               r,
		TaskService:          app.TaskService,
		MeetingService:       app.MeetingService,
		ImportantDateService: app.ImportantDateService,
		IntegrationService:   app.IntegrationService,
		ContactService:       app.ContactService,
		NotificationService:  app.NotificationService,
		CacheService:         app.CacheService,
		QueueService:         app.QueueService,
		QueueServiceOld:      app.QueueServiceOld,
		TaskHandler:          taskHandler,
		StartTime:            time.Now(),
		Logger:               logger,
	}

	return app, nil
}

// setupRoutes настраивает маршруты приложения
func (app *App) setupRoutes() {
	// Middleware
	app.Router.Use(gin.Recovery())
	app.Router.Use(middleware.CORS())
	app.Router.Use(middleware.RateLimit())
	app.Router.Use(middleware.Logging(app.Logger))
	app.Router.Use(middleware.Security())

	// API v1 с аутентификацией
	v1Group := app.Router.Group("/api/v1")
	{
		// Публичные маршруты (без аутентификации)
		v1Group.GET("/health", app.healthCheck)
		v1Group.GET("/docs/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

		// Защищенные маршруты (с аутентификацией)
		protected := v1Group.Group("")
		protected.Use(middleware.AuthMiddleware(app.Config.AuthServiceURL, app.Redis, app.Logger))
		{
			// Tasks
			protected.GET("/tasks", app.TaskHandler.GetTasks)
			protected.POST("/tasks", app.TaskHandler.CreateTask)
			protected.GET("/tasks/:id", app.TaskHandler.GetTask)
			protected.PUT("/tasks/:id", app.TaskHandler.UpdateTask)
			protected.DELETE("/tasks/:id", app.TaskHandler.DeleteTask)

			// Meetings
			protected.GET("/meetings", app.MeetingService.GetMeetings)
			protected.POST("/meetings", app.MeetingService.CreateMeeting)
			protected.GET("/meetings/:id", app.MeetingService.GetMeeting)
			protected.PUT("/meetings/:id", app.MeetingService.UpdateMeeting)
			protected.DELETE("/meetings/:id", app.MeetingService.DeleteMeeting)

			// Important Dates
			protected.GET("/important-dates", app.ImportantDateService.GetImportantDates)
			protected.POST("/important-dates", app.ImportantDateService.CreateImportantDate)
			protected.GET("/important-dates/:id", app.ImportantDateService.GetImportantDate)
			protected.PUT("/important-dates/:id", app.ImportantDateService.UpdateImportantDate)
			protected.DELETE("/important-dates/:id", app.ImportantDateService.DeleteImportantDate)

			// Integrations
			protected.GET("/integrations", app.IntegrationService.GetExternalIntegrations)
			protected.POST("/integrations", app.IntegrationService.CreateExternalIntegration)
			protected.GET("/integrations/:id", app.IntegrationService.GetExternalIntegration)
			protected.PUT("/integrations/:id", app.IntegrationService.UpdateExternalIntegration)
			protected.DELETE("/integrations/:id", app.IntegrationService.DeleteExternalIntegration)
			protected.POST("/integrations/:id/sync", app.IntegrationService.SyncExternalData)

			// Contacts
			protected.GET("/contacts", app.ContactService.GetContacts)
			protected.POST("/contacts", app.ContactService.CreateContact)
			protected.GET("/contacts/:id", app.ContactService.GetContact)
			protected.PUT("/contacts/:id", app.ContactService.UpdateContact)
			protected.DELETE("/contacts/:id", app.ContactService.DeleteContact)

			// Comments
			protected.GET("/tasks/:taskId/comments", app.TaskHandler.GetTaskComments)
			protected.POST("/tasks/:taskId/comments", app.TaskHandler.CreateTaskComment)
			protected.PUT("/tasks/:taskId/comments/:commentId", app.TaskHandler.UpdateTaskComment)
			protected.DELETE("/tasks/:taskId/comments/:commentId", app.TaskHandler.DeleteTaskComment)

			// Subscriptions
			protected.GET("/subscriptions", app.TaskHandler.GetSubscriptions)
			protected.POST("/subscriptions", app.TaskHandler.CreateSubscription)
			protected.DELETE("/subscriptions/:id", app.TaskHandler.DeleteSubscription)

			// Google Calendar
			protected.GET("/google-calendar/auth", app.TaskHandler.GoogleCalendarAuth)
			protected.GET("/google-calendar/callback", app.TaskHandler.GoogleCalendarCallback)
			protected.POST("/google-calendar/sync", app.TaskHandler.GoogleCalendarSync)

			// WebSocket
			protected.GET("/ws", app.handleWebSocket)

			// Analytics
			protected.GET("/analytics/tasks", app.TaskHandler.GetTaskAnalytics)
			protected.GET("/analytics/meetings", app.MeetingService.GetMeetingAnalytics)
			protected.GET("/analytics/contacts", app.ContactService.GetContactAnalytics)

			// Export
			protected.GET("/export/tasks", app.TaskHandler.ExportTasks)
			protected.GET("/export/meetings", app.MeetingService.ExportMeetings)
		}
	}
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
	app.setupRoutes()

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
