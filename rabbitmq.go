package queue

import (
	"encoding/json"
	"log"
	"tasks-service/config"
	"tasks-service/models"
	"tasks-service/services"

	"github.com/streadway/amqp"
)

type QueueService struct {
	Config  config.Config
	Service *services.TaskService
}

func NewQueueService(cfg config.Config, service *services.TaskService) *QueueService {
	return &QueueService{Config: cfg, Service: service}
}

type TaskAction struct {
	Type string                 `json:"type"`
	Data map[string]interface{} `json:"data"`
}

func (qs *QueueService) StartConsumer() {
	conn, err := amqp.Dial(qs.Config.RabbitMQ)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open channel: %v", err)
	}
	defer ch.Close()

	_, err = ch.QueueDeclare("tasks_queue", true, false, false, false, nil)
	if err != nil {
		log.Fatalf("Failed to declare queue: %v", err)
	}

	msgs, err := ch.Consume("tasks_queue", "", false, false, false, false, nil)
	if err != nil {
		log.Fatalf("Failed to register consumer: %v", err)
	}

	go func() {
		for msg := range msgs {
			var task models.Task
			if err := json.Unmarshal(msg.Body, &task); err == nil && task.ID != uuid.Nil {
				// Обновление задачи
				var existingTask models.Task
				if err := qs.Service.DB.First(&existingTask, "id = ?", task.ID).Error; err == nil {
					existingTask.Title = task.Title
					existingTask.Description = task.Description
					existingTask.Status = task.Status
					existingTask.DueDate = task.DueDate
					existingTask.AssignedTo = task.AssignedTo
					qs.Service.DB.Save(&existingTask)
				}
			} else {
				// Удаление задачи
				var deleteData map[string]interface{}
				if err := json.Unmarshal(msg.Body, &deleteData); err == nil {
					if taskID, ok := deleteData["task_id"].(string); ok {
						qs.Service.DB.Delete(&models.Task{}, "id = ?", taskID)
					}
				}
			}
			msg.Ack(false)
		}
	}()
	log.Println("Task consumer started")
}
