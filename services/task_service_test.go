package services

import (
	"context"
	"testing"
	"time"

	"github.com/andre/task-service/models"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/streadway/amqp"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// MockGoogleCalendarService — это фиктивная реализация GoogleCalendarService
type MockGoogleCalendarService struct{}

func (m *MockGoogleCalendarService) CreateEvent(ctx context.Context, taskID, title, description string, dueDate time.Time) (string, error) {
	return "mock-event-id", nil
}

func (m *MockGoogleCalendarService) UpdateEvent(ctx context.Context, taskID, eventID, title, description string, dueDate time.Time) error {
	return nil
}

func (m *MockGoogleCalendarService) DeleteEvent(ctx context.Context, taskID, eventID string) error {
	return nil
}

func setupTestEnvironment(t *testing.T) (*TaskService, func()) {
	// Настройка базы данных SQLite в памяти
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Не удалось открыть тестовую БД: %v", err)
	}

	// Автоматическая миграция всех необходимых моделей
	err = db.AutoMigrate(
		&models.Task{},
		&models.Assignee{},
		&models.Comment{},
		&models.TimeEntry{},
		&models.Payroll{},
		&models.LeaveRequest{},
		&models.OnboardingChecklist{},
	)
	if err != nil {
		t.Fatalf("Не удалось выполнить миграцию базы данных: %v", err)
	}

	// Настройка клиента Redis (используется фиктивный адрес — в реальных тестах нужен тестовый экземпляр Redis)
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	// Фиктивное подключение к RabbitMQ (в продакшене вы бы использовали тестовый контейнер или заглушку)
	rabbitMQConn, err := amqp.Dial("amqp://guest:guest@localhost:5672/")
	if err != nil {
		// Для этого теста продолжаем без RabbitMQ, если подключение не удалось
		t.Logf("Подключение к RabbitMQ не удалось: %v. Продолжаем с пустым подключением", err)
	}
	var rabbitMQChan *amqp.Channel
	if rabbitMQConn != nil {
		rabbitMQChan, err = rabbitMQConn.Channel()
		if err != nil {
			t.Fatalf("Не удалось создать канал RabbitMQ: %v", err)
		}
	}

	// Создание тестового пользователя
	companyID := uuid.New()
	db.Exec("INSERT INTO users (user_id, company_id, role) VALUES (?, ?, ?)", "user123", companyID, "user")
	db.Exec("INSERT INTO users (user_id, company_id, role) VALUES (?, ?, ?)", "admin123", companyID, "admin")

	service := &TaskService{
		DB:             db,
		Redis:          redisClient,
		GoogleCalendar: &MockGoogleCalendarService{},
		RabbitMQConn:   rabbitMQConn,
		RabbitMQChan:   rabbitMQChan,
	}

	// Функция очистки
	cleanup := func() {
		if rabbitMQChan != nil {
			rabbitMQChan.Close()
		}
		if rabbitMQConn != nil {
			rabbitMQConn.Close()
		}
		db.Exec("DROP TABLE tasks")
	}

	return service, cleanup
}

func TestTaskService_CreateTask(t *testing.T) {
	service, cleanup := setupTestEnvironment(t)
	defer cleanup()

	tests := []struct {
		name       string
		userID     string
		task       *models.Task
		wantErr    bool
		errMessage string
	}{
		{
			name:   "Валидная задача",
			userID: "user123",
			task: &models.Task{
				Title:   "Тестовая задача",
				DueDate: time.Now().Add(24 * time.Hour),
				Status:  "pending",
			},
			wantErr: false,
		},
		{
			name:       "Пустое название",
			userID:     "user123",
			task:       &models.Task{},
			wantErr:    true,
			errMessage: "title is required",
		},
		{
			name:   "Недопустимый статус",
			userID: "user123",
			task: &models.Task{
				Title:  "Тестовая задача",
				Status: "invalid",
			},
			wantErr:    true,
			errMessage: "invalid status: invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), "user_id", tt.userID)
			createdTask, err := service.CreateTask(ctx, tt.userID, tt.task, false)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMessage)
				assert.Nil(t, createdTask)
			} else {
				assert.NoError(t, err)
				assert.NotEqual(t, uuid.Nil, createdTask.ID)
				assert.Equal(t, tt.userID, createdTask.CreatedBy)
				assert.Equal(t, tt.task.Title, createdTask.Title)
			}
		})
	}
}

func TestTaskService_GetTasks(t *testing.T) {
	service, cleanup := setupTestEnvironment(t)
	defer cleanup()

	// Настройка тестовых данных
	task1 := &models.Task{
		ID:        uuid.New(),
		Title:     "Тестовая задача 1",
		Status:    "pending",
		DueDate:   time.Now().Add(24 * time.Hour),
		CreatedBy: "user123",
		CreatedAt: time.Now(),
	}
	task2 := &models.Task{
		ID:        uuid.New(),
		Title:     "Тестовая задача 2",
		Status:    "completed",
		DueDate:   time.Now().Add(48 * time.Hour),
		CreatedBy: "user123",
		CreatedAt: time.Now(),
	}
	service.DB.Create(task1)
	service.DB.Create(task2)

	tests := []struct {
		name       string
		status     string
		dueDate    *time.Time
		priority   string
		assigneeID string
		fields     string
		limit      int
		offset     int
		wantTotal  int
		wantCount  int
	}{
		{
			name:      "Получить все задачи в ожидании",
			status:    "pending",
			limit:     10,
			wantTotal: 1,
			wantCount: 1,
		},
		{
			name:      "Получить все задачи",
			limit:     10,
			wantTotal: 2,
			wantCount: 2,
		},
		{
			name:      "Недопустимый статус",
			status:    "invalid",
			limit:     10,
			wantTotal: 0,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), "user_id", "user123")
			tasks, total, err := service.GetTasks(ctx, tt.status, tt.dueDate, tt.priority, tt.assigneeID, tt.fields, tt.limit, tt.offset)

			if tt.name == "Недопустимый статус" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "invalid status")
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantTotal, total)
				assert.Equal(t, tt.wantCount, len(tasks))
				if tt.status != "" && len(tasks) > 0 {
					assert.Equal(t, tt.status, tasks[0].Status)
				}
			}
		})
	}
}

func TestTaskService_UpdateTask(t *testing.T) {
	service, cleanup := setupTestEnvironment(t)
	defer cleanup()

	// Создание начальной задачи
	task := &models.Task{
		ID:        uuid.New(),
		Title:     "Исходная задача",
		Status:    "pending",
		Priority:  1,
		CreatedBy: "user123",
		CreatedAt: time.Now(),
	}
	service.DB.Create(task)

	tests := []struct {
		name       string
		userID     string
		updateTask *models.Task
		wantErr    bool
		errMessage string
	}{
		{
			name:   "Валидное обновление",
			userID: "user123",
			updateTask: &models.Task{
				ID:       task.ID,
				Title:    "Обновленная задача",
				Status:   "in_progress",
				Priority: 2,
			},
			wantErr: false,
		},
		{
			name:   "Недопустимый статус",
			userID: "user123",
			updateTask: &models.Task{
				ID:     task.ID,
				Title:  "Обновленная задача",
				Status: "invalid",
			},
			wantErr:    true,
			errMessage: "invalid status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), "user_id", tt.userID)
			updatedTask, err := service.UpdateTask(ctx, tt.userID, tt.updateTask)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMessage)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.updateTask.Title, updatedTask.Title)
				assert.Equal(t, tt.updateTask.Status, updatedTask.Status)
				assert.Equal(t, tt.updateTask.Priority, updatedTask.Priority)
			}
		})
	}
}

func TestTaskService_DeleteTask(t *testing.T) {
	service, cleanup := setupTestEnvironment(t)
	defer cleanup()

	task := &models.Task{
		ID:        uuid.New(),
		Title:     "Задача для удаления",
		CreatedBy: "user123",
		CreatedAt: time.Now(),
	}
	service.DB.Create(task)

	tests := []struct {
		name    string
		userID  string
		wantErr bool
	}{
		{
			name:    "Создатель удаляет задачу",
			userID:  "user123",
			wantErr: false,
		},
		{
			name:    "Админ удаляет задачу",
			userID:  "admin123",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), "user_id", tt.userID)
			err := service.DeleteTask(ctx, task.ID, tt.userID)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				var deletedTask models.Task
				err := service.DB.First(&deletedTask, task.ID).Error
				assert.Error(t, err) // Задача не должна быть найдена
			}
		})
	}
}
