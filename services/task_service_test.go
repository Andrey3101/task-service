package services

import (
	"context"
	"task-service/models"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) (*gorm.DB, *redis.Client) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test DB: %v", err)
	}
	db.AutoMigrate(&models.Task{})

	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379", // Предполагается, что Redis доступен
	})
	return db, redisClient
}

func TestTaskService_CreateTask(t *testing.T) {
	db, redisClient := setupTestDB(t)
	service := NewTaskService(db, redisClient)

	task := &models.Task{
		Title:      "Test Task",
		DueDate:    time.Now().Add(24 * time.Hour),
		AssignedTo: "user123",
	}
	createdTask, err := service.CreateTask(context.Background(), "user123", task)
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}
	if createdTask.ID == uuid.Nil {
		t.Error("Task ID should not be nil")
	}
	if createdTask.CreatedBy != "user123" {
		t.Errorf("Expected CreatedBy user123, got %s", createdTask.CreatedBy)
	}
}

func TestTaskService_GetTasks(t *testing.T) {
	db, redisClient := setupTestDB(t)
	service := NewTaskService(db, redisClient)

	task := &models.Task{
		ID:         uuid.New(),
		Title:      "Test Task",
		Status:     "pending",
		DueDate:    time.Now().Add(24 * time.Hour),
		AssignedTo: "user123",
		CreatedBy:  "user123",
		CreatedAt:  time.Now(),
	}
	db.Create(task)

	tasks, total, err := service.GetTasks(context.Background(), "pending", nil, 10, 0)
	if err != nil {
		t.Fatalf("Failed to get tasks: %v", err)
	}
	if total != 1 {
		t.Errorf("Expected total 1, got %d", total)
	}
	if len(tasks) != 1 || tasks[0].Title != "Test Task" {
		t.Errorf("Expected task title 'Test Task', got %v", tasks)
	}
}
