package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"tasks-service/config"
	"tasks-service/handlers"
	"tasks-service/middleware"
	"tasks-service/migrations"
	"tasks-service/queue"
	"tasks-service/services"
	"time"

	_ "tasks-service/docs"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
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

	migrations.RunMigrations(cfg.DBConn)

	pool, err := pgxpool.New(context.Background(), cfg.DBConn)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to initialize GORM: %v", err)
	}

	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, PoolSize: 10})
	defer redisClient.Close()

	rabbitConn, err := amqp.Dial(cfg.RabbitMQ)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer rabbitConn.Close()
	rabbitCh, err := rabbitConn.Channel()
	if err != nil {
		log.Fatalf("Failed to open RabbitMQ channel: %v", err)
	}
	defer rabbitCh.Close()

	authService := services.NewAuthService(cfg)
	taskService := services.NewTaskService(db, redisClient, rabbitCh)
	taskHandler := handlers.NewTaskHandler(taskService, cfg)
	queueService := queue.NewQueueService(cfg, taskService)

	go queueService.StartConsumer()

	r := gin.Default()
	r.Use(middleware.AuthMiddleware(authService))

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
	}

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
		shard := taskService.determineUserShard(c.Request.Context(), userID)
		taskService.AuthDBShards[shard].Raw("SELECT user_id, role FROM users WHERE company_id = (SELECT company_id FROM users WHERE user_id = ?)", userID).Scan(&users)
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
			PhoneNumber string `json:"phone_number"`
		}
		if err := c.BindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
			return
		}
		shard := taskService.determineUserShard(c.Request.Context(), userID)
		err := taskService.AuthDBShards[shard].Exec(
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

	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}
