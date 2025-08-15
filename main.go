package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"github.com/andre/task-service/config"
	"github.com/andre/task-service/handlers"
	"github.com/andre/task-service/middleware"
	"github.com/andre/task-service/queue"
	"github.com/andre/task-service/services"
	"github.com/google/uuid"

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
	DBConn       *sql.DB
	DB           *gorm.DB
	Redis        *redis.Client
	RabbitMQ     *amqp.Connection
	RabbitMQChan *amqp.Channel
	Router       *gin.Engine
	TaskService  *services.TaskService
	QueueService *queue.QueueService
	Logger       *zap.Logger
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

// setupDependencies - инициализация зависимостей приложения
func setupDependencies(cfg config.Config) (*App, error) {
	// Инициализация логгера
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, err
	}

	// Подключение к базе данных
	dbConn, err := sql.Open("pgx", cfg.DBConn)
	if err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
		return nil, err
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: dbConn}), &gorm.Config{})
	if err != nil {
		logger.Fatal("Failed to initialize GORM", zap.Error(err))
		return nil, err
	}

	// Подключение к Redis
	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, PoolSize: 10})
	if _, err := redisClient.Ping(context.Background()).Result(); err != nil {
		logger.Fatal("Failed to connect to Redis", zap.Error(err))
		return nil, err
	}

	// Подключение к RabbitMQ с ретраями
	var rabbitConn *amqp.Connection
	for i := 0; i < 10; i++ {
		rabbitConn, err = amqp.Dial(cfg.RabbitMQ)
		if err == nil {
			break
		}
		logger.Warn("Failed to connect to RabbitMQ", zap.Int("attempt", i+1), zap.Error(err))
		time.Sleep(5 * time.Second)
	}
	if err != nil {
		logger.Fatal("Failed to connect to RabbitMQ after retries", zap.Error(err))
		return nil, err
	}
	rabbitCh, err := rabbitConn.Channel()
	if err != nil {
		logger.Fatal("Failed to open RabbitMQ channel", zap.Error(err))
		return nil, err
	}

	// Инициализация сервисов
	googleCalendar := services.NewGoogleCalendarService(cfg, redisClient)
	taskService, err := services.NewTaskService(db, redisClient, googleCalendar, cfg.RabbitMQ)
	if err != nil {
		logger.Fatal("Failed to initialize TaskService", zap.Error(err))
		return nil, err
	}
	queueService := queue.NewQueueService(cfg, taskService)
	taskHandler := handlers.NewTaskHandler(taskService, cfg)

	// Настройка маршрутизатора
	r := gin.Default()
	r.Use(middleware.CORSMiddleware())
	r.Use(middleware.RateLimitMiddleware())
	r.Use(middleware.LoggingMiddleware(logger))
	r.Use(middleware.AuthMiddleware(cfg))

	app := &App{
		DBConn:       dbConn, // Сохраняем *sql.DB для закрытия
		DB:           db,
		Redis:        redisClient,
		RabbitMQ:     rabbitConn,
		RabbitMQChan: rabbitCh,
		Router:       r,
		TaskService:  taskService,
		QueueService: queueService,
		Logger:       logger,
	}

	// Запуск потребителя RabbitMQ
	go queueService.StartConsumer()

	// Регистрация маршрутов
	app.registerRoutes(taskHandler)

	return app, nil
}

// registerRoutes - регистрация всех маршрутов
func (app *App) registerRoutes(h *handlers.TaskHandler) {
	r := app.Router

	// WebSocket для обновлений задач
	r.GET("/ws/tasks", func(c *gin.Context) {
		app.handleTaskUpdatesWebSocket(c)
	})

	// WebSocket для чата задач
	r.GET("/ws/tasks/:task_id/chat", func(c *gin.Context) {
		app.handleTaskChatWebSocket(c)
	})

	// Эндпоинты Google Calendar
	r.GET("/google/auth", func(c *gin.Context) {
		url := app.TaskService.GoogleCalendar.GetAuthURL()
		c.JSON(http.StatusOK, gin.H{"url": url})
	})
	r.GET("/google/callback", func(c *gin.Context) {
		code := c.Query("code")
		if err := app.TaskService.GoogleCalendar.Authenticate(c.Request.Context(), code); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to authenticate with Google"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Google Calendar authenticated"})
	})
	r.GET("/google/events", func(c *gin.Context) {
		events, err := app.TaskService.GoogleCalendar.GetCalendarEvents(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		eventList := make([]map[string]interface{}, 0, len(events))
		for _, event := range events {
			eventList = append(eventList, map[string]interface{}{
				"id":          event.Id,
				"summary":     event.Summary,
				"description": event.Description,
				"start":       event.Start.DateTime,
				"end":         event.End.DateTime,
			})
		}
		c.JSON(http.StatusOK, gin.H{"events": eventList})
	})
	r.POST("/google/events", func(c *gin.Context) {
		var input struct {
			Summary     string `json:"summary" binding:"required,max=255"`
			Description string `json:"description" binding:"max=1000"`
			Start       string `json:"start" binding:"required"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		eventID, err := app.TaskService.GoogleCalendar.CreateCalendarEvent(c.Request.Context(), input.Summary, input.Description, input.Start)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"event_id": eventID})
	})
	r.PUT("/google/events/:id", func(c *gin.Context) {
		eventID := c.Param("id")
		var input struct {
			Summary     string `json:"summary" binding:"required,max=255"`
			Description string `json:"description" binding:"max=1000"`
			Start       string `json:"start" binding:"required"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		updatedEventID, err := app.TaskService.GoogleCalendar.UpdateCalendarEvent(c.Request.Context(), eventID, input.Summary, input.Description, input.Start)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"event_id": updatedEventID})
	})
	r.DELETE("/google/events/:id", func(c *gin.Context) {
		eventID := c.Param("id")
		if err := app.TaskService.GoogleCalendar.DeleteCalendarEvent(c.Request.Context(), eventID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Event deleted"})
	})

	// Эндпоинты задач
	api := r.Group("/tasks")
	{
		// Специфичные маршруты (должны идти перед параметризованными)
		api.POST("/create", h.CreateTask)
		api.POST("/create/predictive", h.CreateTaskWithPrediction)
		api.POST("/report", h.GenerateReport)
		api.GET("/reports", h.GetReports)
		api.GET("/dashboard", h.GetDashboard)
		api.GET("/my", h.GetMyTasks)
		api.GET("/suggestions", h.GetTaskSuggestions)
		api.POST("/report/quick", h.GenerateQuickReport)
		api.POST("/bulk_assign", h.BulkAssignTasks)
		api.GET("/task", h.GetTaskStats)
		api.POST("/follow-up", h.CreateFollowUpTask)
		api.GET("/sales/analytics", h.GetSalesAnalytics)
		api.POST("/time/stop", h.StopTimeTracking)
		api.GET("/time/active", h.GetActiveTimeEntries)
		api.GET("/time/total", h.CalculateTotalActiveTime)
		api.POST("/payroll/calculate", h.CalculatePayroll)
		api.GET("/payroll/aggregate", h.GetAggregatePayroll)
		api.POST("/onboarding", h.CreateOnboardingChecklist)
		api.POST("/leave/request", h.CreateLeaveRequest)
		api.POST("/recurring", h.CreateRecurringTask)
		api.GET("/productivity", h.GetProductivityReport)
		api.POST("/tax-reminders", h.CreateTaxReminders)
		
		// Параметризованные маршруты
		api.GET("", h.GetTasks)
		api.GET("/:id", h.GetTask)
		api.PUT("/:id", h.UpdateTask)
		api.DELETE("/:id", h.DeleteTask)
		api.POST("/:id/comments", h.AddComment)
		api.PUT("/:id/comments/:comment_id", h.UpdateComment)
		api.DELETE("/:id/comments/:comment_id", h.DeleteComment)
		api.POST("/:task_id/time/start", h.StartTimeTracking)
		api.GET("/users/:user_id/time-report", h.GetTimeReport)
	}

		api.POST("/financial-events", func(c *gin.Context) {
			var event services.FinancialEvent
			if err := c.ShouldBindJSON(&event); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			userID := c.GetString("user_id")
			createdEvent, err := app.TaskService.CreateFinancialEvent(c.Request.Context(), userID, &event)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, createdEvent)
		})

		api.GET("/financial-events", func(c *gin.Context) {
			userID := c.GetString("user_id")
			var startDate, endDate *time.Time
			if startStr := c.Query("start_date"); startStr != "" {
				if t, err := time.Parse("2006-01-02", startStr); err == nil {
					startDate = &t
				}
			}
			if endStr := c.Query("end_date"); endStr != "" {
				if t, err := time.Parse("2006-01-02", endStr); err == nil {
					endDate = &t
				}
			}
			events, err := app.TaskService.GetFinancialEvents(c.Request.Context(), userID, startDate, endDate)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, events)
		})
	}

	// Эндпоинты управления пользователями
	api.GET("/users", func(c *gin.Context) {
		userID := c.GetString("user_id")
		role := c.GetString("role")
		if role != "manager" && role != "admin" && role != "super_admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "Permission denied"})
			return
		}
		var users []struct {
			UserID string `gorm:"column:user_id"`
			Role   string `gorm:"column:role"`
		}
		app.DB.Raw("SELECT user_id, role FROM users WHERE company_id = (SELECT company_id FROM users WHERE user_id = ?)", userID).Scan(&users)
		c.JSON(http.StatusOK, users)
	})

	api.PUT("/users/:id", func(c *gin.Context) {
		userID := c.GetString("user_id")
		role := c.GetString("role")
		if role != "manager" && role != "admin" && role != "super_admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "Permission denied"})
			return
		}
		targetID := c.Param("id")
		var input struct {
			PhoneNumber string `json:"phone_number" binding:"max=20"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
			return
		}
		if input.PhoneNumber != "" && !regexp.MustCompile(`^\+?\d{7,20}$`).MatchString(input.PhoneNumber) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid phone number format"})
			return
		}
		err := app.DB.Exec(
			"UPDATE users SET phone_number = ? WHERE user_id = ? AND company_id = (SELECT company_id FROM users WHERE user_id = ?)",
			input.PhoneNumber, targetID, userID).Error
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Server error"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "User updated"})
	})

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	})
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
}

// handleTaskUpdatesWebSocket - обработчик WebSocket для обновлений задач
func (app *App) handleTaskUpdatesWebSocket(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		app.Logger.Error("WebSocket upgrade failed", zap.Error(err))
		return
	}
	defer conn.Close()

	// Установка тайм-аутов и обработчиков ping/pong
	conn.SetReadDeadline(time.Now().Add(WebsocketReadDeadline))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(WebsocketReadDeadline))
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pubsub := app.Redis.Subscribe(ctx, "task_updates")
	defer pubsub.Close()
	redisChan := pubsub.Channel()

	var partitionFilter string
	go func() {
		defer cancel()
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				app.Logger.Error("WebSocket read error", zap.Error(err))
				return
			}
			var filter struct {
				Partition string `json:"partition"`
			}
			if err := json.Unmarshal(message, &filter); err == nil && filter.Partition != "" {
				partitionFilter = filter.Partition
				app.Logger.Info("Client subscribed to partition", zap.String("partition", partitionFilter))
			}
		}
	}()

	// Периодическая отправка ping-сообщений
	go func() {
		ticker := time.NewTicker(WebsocketPingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(WebsocketWriteDeadline)); err != nil {
					app.Logger.Error("WebSocket ping error", zap.Error(err))
					return
				}
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-redisChan:
			if !ok {
				return
			}
			var taskEvent struct {
				TaskID    string    `json:"task_id"`
				Action    string    `json:"action"`
				CreatedAt time.Time `json:"created_at"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &taskEvent); err != nil {
				continue
			}
			if partitionFilter != "" && taskEvent.CreatedAt.Format("2006-01") != partitionFilter {
				continue
			}
			conn.SetWriteDeadline(time.Now().Add(WebsocketWriteDeadline))
			if err := conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload)); err != nil {
				app.Logger.Error("WebSocket write error", zap.Error(err))
				return
			}
		}
	}
}

// handleTaskChatWebSocket - обработчик WebSocket для чата задач
func (app *App) handleTaskChatWebSocket(c *gin.Context) {
	taskID, _ := uuid.Parse(c.Param("task_id"))
	userID := c.GetString("user_id")
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		app.Logger.Error("WebSocket upgrade failed", zap.Error(err))
		return
	}
	defer conn.Close()

	// Установка тайм-аутов и обработчиков ping/pong
	conn.SetReadDeadline(time.Now().Add(WebsocketReadDeadline))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(WebsocketReadDeadline))
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pubsub := app.Redis.Subscribe(ctx, "task_chat:"+taskID.String())
	defer pubsub.Close()
	redisChan := pubsub.Channel()

	// Отправка существующих комментариев
	comments, _ := app.TaskService.GetTaskComments(c.Request.Context(), taskID)
	for _, comment := range comments {
		commentJSON, _ := json.Marshal(comment)
		conn.SetWriteDeadline(time.Now().Add(WebsocketWriteDeadline))
		if err := conn.WriteMessage(websocket.TextMessage, commentJSON); err != nil {
			app.Logger.Error("WebSocket write error", zap.Error(err))
			return
		}
	}

	go func() {
		defer cancel()
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				app.Logger.Error("WebSocket read error", zap.Error(err))
				return
			}
			var input struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(message, &input); err == nil {
				comment, _ := app.TaskService.AddComment(c.Request.Context(), taskID, userID, input.Text)
				commentJSON, _ := json.Marshal(comment)
				app.TaskService.Redis.Publish(c.Request.Context(), "task_chat:"+taskID.String(), commentJSON)
			}
		}
	}()

	// Периодическая отправка ping-сообщений
	go func() {
		ticker := time.NewTicker(WebsocketPingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(WebsocketWriteDeadline)); err != nil {
					app.Logger.Error("WebSocket ping error", zap.Error(err))
					return
				}
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-redisChan:
			if !ok {
				return
			}
			conn.SetWriteDeadline(time.Now().Add(WebsocketWriteDeadline))
			if err := conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload)); err != nil {
				app.Logger.Error("WebSocket write error", zap.Error(err))
				return
			}
		}
	}
}

// Close - закрытие всех ресурсов приложения
func (app *App) Close() {
	app.Logger.Info("Shutting down application")
	app.DBConn.Close()
	app.Redis.Close()
	app.RabbitMQChan.Close()
	app.RabbitMQ.Close()
	app.TaskService.Close()
	app.Logger.Sync()
}

func main() {
	cfg := config.LoadConfig()

	app, err := setupDependencies(cfg)
	if err != nil {
		log.Fatalf("Failed to setup dependencies: %v", err)
	}
	defer app.Close()

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: app.Router,
	}

	// Запуск сервера в горутине
	go func() {
		app.Logger.Info("Starting server", zap.String("port", cfg.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			app.Logger.Fatal("Server failed", zap.Error(err))
		}
	}()

	// Ожидание сигнала для graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	app.Logger.Info("Received shutdown signal")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		app.Logger.Error("Server shutdown failed", zap.Error(err))
	}
	app.Close()
}
