package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
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
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func main() {
	cfg := config.LoadConfig()
	log.Printf("Connecting to RabbitMQ at: %s", cfg.RabbitMQ)

	// Подключение к базе данных через database/sql
	dbConn, err := sql.Open("pgx", cfg.DBConn)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer dbConn.Close()

	// Настройка GORM с использованием database/sql
	db, err := gorm.Open(postgres.New(postgres.Config{
		Conn: dbConn,
	}), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to initialize GORM: %v", err)
	}

	// Подключение к Redis
	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, PoolSize: 10})
	defer redisClient.Close()

	// Подключение к RabbitMQ
	var rabbitConn *amqp.Connection
	for i := 0; i < 10; i++ {
		rabbitConn, err = amqp.Dial(cfg.RabbitMQ)
		if err == nil {
			break
		}
		log.Printf("Failed to connect to RabbitMQ (attempt %d): %v", i+1, err)
		time.Sleep(5 * time.Second) // Ожидание перед следующей попыткой
	}
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ after retries: %v", err)
	}
	defer rabbitConn.Close()
	rabbitCh, err := rabbitConn.Channel()
	if err != nil {
		log.Fatalf("Failed to open RabbitMQ channel: %v", err)
	}
	defer rabbitCh.Close()

	// Инициализация сервисов
	authService := services.NewAuthService(cfg, redisClient, db)
	taskService := services.NewTaskService(db, redisClient, rabbitCh, cfg)
	taskHandler := handlers.NewTaskHandler(taskService, cfg)
	queueService := queue.NewQueueService(cfg, taskService)

	// Запуск потребителя очереди в фоновом режиме
	go queueService.StartConsumer()

	// Настройка Gin-роутера
	r := gin.Default()
	r.Use(middleware.AuthMiddleware(authService))

	// WebSocket для обновлений задач
	r.GET("/ws/tasks", func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		pubsub := redisClient.Subscribe(context.Background(), "task_updates")
		defer pubsub.Close()
		redisChan := pubsub.Channel()

		var partitionFilter string
		go func() {
			for {
				_, message, err := conn.ReadMessage()
				if err != nil {
					log.Printf("WebSocket read error: %v", err)
					break
				}
				var filter struct {
					Partition string `json:"partition"`
				}
				if err := json.Unmarshal(message, &filter); err == nil && filter.Partition != "" {
					partitionFilter = filter.Partition
					log.Printf("Client subscribed to partition: %s", partitionFilter)
				}
			}
		}()

		for msg := range redisChan {
			var taskEvent struct {
				TaskID    string    `json:"task_id"`
				Action    string    `json:"action,omitempty"`
				CreatedAt time.Time `json:"created_at,omitempty"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &taskEvent); err != nil {
				continue
			}
			if partitionFilter != "" {
				partition := taskEvent.CreatedAt.Format("2006-01")
				if partition != partitionFilter {
					continue
				}
			}
			if err := conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload)); err != nil {
				log.Printf("WebSocket write error: %v", err)
				break
			}
		}
	})

	// Эндпоинты Google Calendar
	r.GET("/google/auth", func(c *gin.Context) {
		url := taskService.GoogleCalendar.GetAuthURL()
		c.JSON(http.StatusOK, gin.H{"url": url})
	})

	r.GET("/google/callback", func(c *gin.Context) {
		code := c.Query("code")
		if err := taskService.GoogleCalendar.Authenticate(c.Request.Context(), code); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to authenticate with Google"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Google Calendar authenticated"})
	})

	r.GET("/google/events", func(c *gin.Context) {
		events, err := taskService.GoogleCalendar.GetCalendarEvents(c.Request.Context())
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
		eventID, err := taskService.GoogleCalendar.CreateCalendarEvent(c.Request.Context(), input.Summary, input.Description, input.Start)
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
		updatedEventID, err := taskService.GoogleCalendar.UpdateCalendarEvent(c.Request.Context(), eventID, input.Summary, input.Description, input.Start)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"event_id": updatedEventID})
	})

	r.DELETE("/google/events/:id", func(c *gin.Context) {
		eventID := c.Param("id")
		if err := taskService.GoogleCalendar.DeleteCalendarEvent(c.Request.Context(), eventID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Event deleted"})
	})

	r.GET("/ws/tasks/:task_id/chat", func(c *gin.Context) {
		taskID, _ := uuid.Parse(c.Param("task_id"))
		userID := c.GetString("user_id")
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		pubsub := redisClient.Subscribe(context.Background(), "task_chat:"+taskID.String())
		defer pubsub.Close()
		redisChan := pubsub.Channel()

		comments, _ := taskService.GetTaskComments(c.Request.Context(), taskID)
		for _, comment := range comments {
			commentJSON, _ := json.Marshal(comment)
			conn.WriteMessage(websocket.TextMessage, commentJSON)
		}

		go func() {
			for {
				_, message, err := conn.ReadMessage()
				if err != nil {
					log.Printf("WebSocket read error: %v", err)
					break
				}
				var input struct {
					Text string `json:"text"`
				}
				if err := json.Unmarshal(message, &input); err == nil {
					comment, _ := taskService.AddComment(c.Request.Context(), taskID, userID, input.Text)
					commentJSON, _ := json.Marshal(comment)
					taskService.Redis.Publish(c.Request.Context(), "task_chat:"+taskID.String(), commentJSON)
				}
			}
		}()

		for msg := range redisChan {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload)); err != nil {
				log.Printf("WebSocket write error: %v", err)
				break
			}
		}
	})

	// Эндпоинты задач
	api := r.Group("/tasks")
	{
		api.POST("/create", taskHandler.CreateTask)
		api.GET("", taskHandler.GetTasks)
		api.PUT("/:id", taskHandler.UpdateTask)
		api.DELETE("/:id", taskHandler.DeleteTask)
		api.POST("/:id/comments", taskHandler.AddComment)
		api.POST("/report", taskHandler.GenerateReport)
		api.GET("/reports", taskHandler.GetReports)
		api.POST("/create/predictive", taskHandler.CreateTaskWithPrediction)
		api.GET("/dashboard", taskHandler.GetDashboard)
		api.GET("/my", taskHandler.GetMyTasks)
		api.GET("/suggestions", taskHandler.GetTaskSuggestions)
		api.POST("/report/quick", taskHandler.GenerateQuickReport)
		api.POST("/bulk_assign", taskHandler.BulkAssignTasks)
		api.GET("/:id", taskHandler.GetTask)
		api.PUT("/:id/comments/:comment_id", taskHandler.UpdateComment)
		api.DELETE("/:id/comments/:comment_id", taskHandler.DeleteComment)
		api.GET("/task", taskHandler.GetTaskStats)
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
		db.Raw("SELECT user_id, role FROM users WHERE company_id = (SELECT company_id FROM users WHERE user_id = ?)", userID).Scan(&users)
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
		err := db.Exec(
			"UPDATE users SET phone_number = ? WHERE user_id = ? AND company_id = (SELECT company_id FROM users WHERE user_id = ?)",
			input.PhoneNumber, targetID, userID).Error
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Server error"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "User updated"})
	})

	// Внутри группы api в main.go
	api.POST("/financial-events", func(c *gin.Context) {
		var event FinancialEvent
		if err := c.ShouldBindJSON(&event); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		userID := c.GetString("user_id")
		createdEvent, err := taskService.CreateFinancialEvent(c.Request.Context(), userID, &event)
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
		events, err := taskService.GetFinancialEvents(c.Request.Context(), userID, startDate, endDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, events)
	})

	api.POST("/:task_id/time/start", func(c *gin.Context) {
		taskID, _ := uuid.Parse(c.Param("task_id"))
		userID := c.GetString("user_id")
		entry, err := taskService.StartTimeTracking(c.Request.Context(), taskID, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, entry)
	})

	api.POST("/:task_id/time/stop", func(c *gin.Context) {
		entryID, _ := uuid.Parse(c.Query("entry_id"))
		userID := c.GetString("user_id")
		entry, err := taskService.StopTimeTracking(c.Request.Context(), entryID, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, entry)
	})

	api.GET("/users/:user_id/time-report", func(c *gin.Context) {
		userID := c.Param("user_id")
		period := c.Query("period")
		entries, err := taskService.GetTimeReport(c.Request.Context(), userID, period)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, entries)
	})

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	})
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}
