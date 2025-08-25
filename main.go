package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
	"github.com/rabbitmq/amqp091-go"
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
	RabbitMQ             *amqp091.Connection
	RabbitMQChan         *amqp091.Channel
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
		Addr:     cfg.Redis.GetAddr(),
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	// Проверка подключения к Redis
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		logger.Fatal("Failed to connect to Redis", zap.Error(err))
		return nil, err
	}

	// Подключение к RabbitMQ
	rabbitMQConn, err := amqp091.Dial(cfg.RabbitMQ.URL)
	if err != nil {
		logger.Fatal("Failed to connect to RabbitMQ", zap.Error(err))
		return nil, err
	}

	rabbitMQChan, err := rabbitMQConn.Channel()
	if err != nil {
		logger.Fatal("Failed to open RabbitMQ channel", zap.Error(err))
		return nil, err
	}

	// Инициализация сервисов
	calendarService := services.NewCalendarService(db, redisClient)
	taskService, err := services.NewTaskService(db, redisClient, calendarService, cfg.RabbitMQ.URL)
	if err != nil {
		logger.Fatal("Failed to create task service", zap.Error(err))
		return nil, err
	}

	meetingService := services.NewMeetingService(db)
	importantDateService := services.NewImportantDateService(db)
	integrationService := services.NewIntegrationService(db)
	contactService := services.NewContactService(db)
	notificationService := services.NewNotificationService(cfg, logger)
	cacheService := services.NewCacheService(redisClient, logger)
	queueService, err := services.NewQueueService(cfg, logger)
	if err != nil {
		logger.Fatal("Failed to create queue service", zap.Error(err))
		return nil, err
	}

	// Инициализация хендлеров
	taskHandler := handlers.NewTaskHandler(taskService, *cfg)
	meetingHandler := handlers.NewMeetingHandler(logger)
	importantDateHandler := handlers.NewImportantDateHandler(logger)
	integrationHandler := handlers.NewIntegrationHandler(integrationService, calendarService, logger)
	contactHandler := handlers.NewContactHandler(logger)
	calendarHandler := handlers.NewCalendarHandler(calendarService, logger)

	// Новые хендлеры для инновационных функций
	aiHandler := handlers.NewAIHandler(logger)
	reportHandler := handlers.NewReportHandler(logger)
	botHandler := handlers.NewBotHandler(logger)
	vrHandler := handlers.NewVRHandler(logger)
	mobileHandler := handlers.NewMobileHandler(logger)

	// Создаем notificationHandler для уведомлений
	notificationHandler := handlers.NewNotificationHandler(logger)

	// Инициализация роутера
	router := gin.Default()
	router.Use(gin.Recovery())

	// Настройка CORS
	router.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// API v1 роуты
	v1 := router.Group("/api/v1")
	{
		// Задачи
		tasks := v1.Group("/tasks")
		{
			tasks.POST("/", taskHandler.CreateTask)
			tasks.GET("/", taskHandler.GetTasks)
			tasks.GET("/:id", taskHandler.GetTask)
			tasks.PUT("/:id", taskHandler.UpdateTask)
			tasks.DELETE("/:id", taskHandler.DeleteTask)
			tasks.POST("/:id/comments", taskHandler.AddComment)
		}

		// Встречи
		meetings := v1.Group("/meetings")
		{
			meetings.POST("/", meetingHandler.CreateMeeting)
			meetings.GET("/", meetingHandler.GetMeetings)
			meetings.GET("/:id", meetingHandler.GetMeeting)
			meetings.PUT("/:id", meetingHandler.UpdateMeeting)
			meetings.DELETE("/:id", meetingHandler.DeleteMeeting)
		}

		// Важные даты
		importantDates := v1.Group("/important-dates")
		{
			importantDates.POST("/", importantDateHandler.CreateImportantDate)
			importantDates.GET("/", importantDateHandler.GetImportantDates)
			importantDates.GET("/:id", importantDateHandler.GetImportantDate)
			importantDates.PUT("/:id", importantDateHandler.UpdateImportantDate)
			importantDates.DELETE("/:id", importantDateHandler.DeleteImportantDate)
		}

		// Интеграции
		integrations := v1.Group("/integrations")
		{
			integrations.POST("/", integrationHandler.CreateExternalIntegration)
			integrations.GET("/", integrationHandler.GetExternalIntegrations)
			integrations.GET("/:id", integrationHandler.GetExternalIntegration)
			integrations.PUT("/:id", integrationHandler.UpdateExternalIntegration)
			integrations.DELETE("/:id", integrationHandler.DeleteExternalIntegration)
			integrations.POST("/calendar/connect", integrationHandler.ConnectCalendar)
			integrations.DELETE("/calendar/:id", integrationHandler.DisconnectCalendar)
			integrations.POST("/calendar/:user_id/sync", integrationHandler.SyncCalendar)
		}

		// Контакты
		contacts := v1.Group("/contacts")
		{
			contacts.POST("/", contactHandler.CreateContact)
			contacts.GET("/", contactHandler.GetContacts)
			contacts.GET("/:id", contactHandler.GetContact)
			contacts.PUT("/:id", contactHandler.UpdateContact)
			contacts.DELETE("/:id", contactHandler.DeleteContact)
		}

		// Календарь
		calendar := v1.Group("/calendar")
		{
			calendar.POST("/events", calendarHandler.CreateEvent)
			calendar.GET("/events/:id", calendarHandler.GetEvent)
			calendar.GET("/events", calendarHandler.GetUserEvents)
			calendar.PUT("/events/:id", calendarHandler.UpdateEvent)
			calendar.DELETE("/events/:id", calendarHandler.DeleteEvent)
			calendar.GET("/settings", calendarHandler.GetUserCalendarSettings)
			calendar.PUT("/settings", calendarHandler.UpdateUserCalendarSettings)
			calendar.POST("/events/:id/reminders", calendarHandler.AddReminder)
			calendar.GET("/tags", calendarHandler.GetUserTags)
			calendar.POST("/tags", calendarHandler.CreateTag)
			calendar.POST("/sync/tasks", calendarHandler.SyncTasksToCalendar)
		}

		// AI и аналитика
		ai := v1.Group("/ai")
		{
			ai.POST("/analyze-productivity", aiHandler.AnalyzeProductivity)
			ai.GET("/insights", aiHandler.GetInsights)
			ai.POST("/recommendations", aiHandler.GenerateRecommendations)
			ai.POST("/predict-tasks", aiHandler.PredictTaskCompletion)
			ai.GET("/patterns", aiHandler.GetBehaviorPatterns)
		}

		// Умные уведомления
		notifications := v1.Group("/notifications")
		{
			notifications.GET("/", notificationHandler.GetNotifications)
			notifications.GET("/:id", notificationHandler.GetNotification)
			notifications.PUT("/:id/read", notificationHandler.MarkAsRead)
			notifications.PUT("/:id/priority", notificationHandler.UpdatePriority)
			notifications.DELETE("/:id", notificationHandler.DeleteNotification)
			notifications.POST("/batch/read", notificationHandler.MarkBatchAsRead)
			notifications.GET("/ai/patterns", notificationHandler.GetAIPatterns)
			notifications.GET("/ai/recommendations", notificationHandler.GetAIRecommendations)
		}

		// Персональные отчеты
		reports := v1.Group("/reports")
		{
			reports.POST("/", reportHandler.CreateReport)
			reports.GET("/", reportHandler.GetReports)
			reports.GET("/:id", reportHandler.GetReport)
			reports.PUT("/:id", reportHandler.UpdateReport)
			reports.DELETE("/:id", reportHandler.DeleteReport)
			reports.POST("/:id/export", reportHandler.ExportReport)
			reports.POST("/ai/generate", reportHandler.GenerateAIReport)
			reports.GET("/templates", reportHandler.GetReportTemplates)
		}

		// Умный бот
		bot := v1.Group("/bot")
		{
			bot.POST("/chat", botHandler.ProcessMessage)
			bot.GET("/context", botHandler.GetUserContext)
			bot.POST("/learn", botHandler.LearnFromInteraction)
			bot.GET("/suggestions", botHandler.GetSuggestions)
			bot.POST("/actions", botHandler.ExecuteAction)
		}

		// VR/AR функции
		vr := v1.Group("/vr")
		{
			vr.POST("/boards", vrHandler.CreateBoard)
			vr.GET("/boards", vrHandler.GetBoards)
			vr.GET("/boards/:id", vrHandler.GetBoard)
			vr.PUT("/boards/:id", vrHandler.UpdateBoard)
			vr.DELETE("/boards/:id", vrHandler.DeleteBoard)
			vr.POST("/boards/:id/export", vrHandler.ExportBoard)
			vr.POST("/boards/:id/share", vrHandler.ShareBoard)
		}

		// Мобильное приложение
		mobile := v1.Group("/mobile")
		{
			mobile.POST("/sync", mobileHandler.SyncData)
			mobile.GET("/offline-data", mobileHandler.GetOfflineData)
			mobile.POST("/voice-notes", mobileHandler.UploadVoiceNote)
			mobile.GET("/qr-scan", mobileHandler.ProcessQRCode)
			mobile.POST("/location", mobileHandler.UpdateLocation)
			mobile.GET("/push-subscription", mobileHandler.GetPushSubscription)
		}
	}

	// Swagger документация
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "ok",
			"timestamp": time.Now().Unix(),
			"service":   "task-service",
			"version":   "1.0.0",
		})
	})

	// WebSocket endpoint для real-time уведомлений
	router.GET("/ws", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "WebSocket endpoint - not implemented yet",
			"status":  "coming_soon",
		})
	})

	return &App{
		Config:               cfg,
		DBConn:               dbConn,
		DB:                   db,
		Redis:                redisClient,
		RabbitMQ:             rabbitMQConn,
		RabbitMQChan:         rabbitMQChan,
		Router:               router,
		TaskService:          taskService,
		MeetingService:       meetingService,
		ImportantDateService: importantDateService,
		IntegrationService:   integrationService,
		ContactService:       contactService,
		NotificationService:  notificationService,
		CacheService:         cacheService,
		QueueService:         queueService,
		TaskHandler:          taskHandler,
		StartTime:            time.Now(),
		Logger:               logger,
	}, nil
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
		protected.Use(middleware.AuthMiddleware(app.Config.Auth.ServiceURL, app.Redis, app.Logger))
		{
			// Используем единую структуру маршрутов из v1.SetupRoutes
			v1.SetupRoutes(app.Router, app.TaskHandler)

			// WebSocket
			protected.GET("/ws", app.handleWebSocket)
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
	// Настройка HTTP сервера
	server := &http.Server{
		Addr:         fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port),
		Handler:      app.Router,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
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
		logger.Error("Failed to setup dependencies", zap.Error(err))
		log.Fatal("Failed to setup dependencies", err)
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
