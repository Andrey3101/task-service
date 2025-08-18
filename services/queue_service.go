package services

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/andre/task-service/config"
	"github.com/streadway/amqp"
	"go.uber.org/zap"
)

// QueueMessage структура сообщения в очереди
type QueueMessage struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	Data       map[string]interface{} `json:"data"`
	Timestamp  time.Time              `json:"timestamp"`
	RetryCount int                    `json:"retry_count"`
}

// QueueService сервис для работы с очередями RabbitMQ
type QueueService struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	config  *config.Config
	logger  *zap.Logger
}

// NewQueueService создает новый сервис очередей
func NewQueueService(cfg *config.Config, logger *zap.Logger) (*QueueService, error) {
	// Подключение к RabbitMQ
	conn, err := amqp.Dial(cfg.RabbitMQ.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	// Создание канала
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}

	// Объявление очередей
	if err := ch.ExchangeDeclare(
		"task_service", // name
		"topic",        // type
		true,           // durable
		false,          // auto-deleted
		false,          // internal
		false,          // no-wait
		nil,            // arguments
	); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare exchange: %w", err)
	}

	// Очередь для встреч
	meetingQueue, err := ch.QueueDeclare(
		"meeting_queue", // name
		true,            // durable
		false,           // delete when unused
		false,           // exclusive
		false,           // no-wait
		nil,             // arguments
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare meeting queue: %w", err)
	}

	// Привязка очереди встреч к exchange
	if err := ch.QueueBind(
		meetingQueue.Name, // queue name
		"meeting.*",       // routing key
		"task_service",    // exchange
		false,
		nil,
	); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to bind meeting queue: %w", err)
	}

	// Очередь для важных дат
	dateQueue, err := ch.QueueDeclare(
		"important_date_queue", // name
		true,                   // durable
		false,                  // delete when unused
		false,                  // exclusive
		false,                  // no-wait
		nil,                    // arguments
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare important date queue: %w", err)
	}

	// Привязка очереди важных дат к exchange
	if err := ch.QueueBind(
		dateQueue.Name,     // queue name
		"important_date.*", // routing key
		"task_service",     // exchange
		false,
		nil,
	); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to bind important date queue: %w", err)
	}

	// Очередь для интеграций
	integrationQueue, err := ch.QueueDeclare(
		"integration_queue", // name
		true,                // durable
		false,               // delete when unused
		false,               // exclusive
		false,               // no-wait
		nil,                 // arguments
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare integration queue: %w", err)
	}

	// Привязка очереди интеграций к exchange
	if err := ch.QueueBind(
		integrationQueue.Name, // queue name
		"integration.*",       // routing key
		"task_service",        // exchange
		false,
		nil,
	); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to bind integration queue: %w", err)
	}

	// Очередь для контактов
	contactQueue, err := ch.QueueDeclare(
		"contact_queue", // name
		true,            // durable
		false,           // delete when unused
		false,           // exclusive
		false,           // no-wait
		nil,             // arguments
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare contact queue: %w", err)
	}

	// Привязка очереди контактов к exchange
	if err := ch.QueueBind(
		contactQueue.Name, // queue name
		"contact.*",       // routing key
		"task_service",    // exchange
		false,
		nil,
	); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to bind contact queue: %w", err)
	}

	// Очередь для уведомлений
	notificationQueue, err := ch.QueueDeclare(
		"notification_queue", // name
		true,                 // durable
		false,                // delete when unused
		false,                // exclusive
		false,                // no-wait
		nil,                  // arguments
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare notification queue: %w", err)
	}

	// Привязка очереди уведомлений к exchange
	if err := ch.QueueBind(
		notificationQueue.Name, // queue name
		"notification.*",       // routing key
		"task_service",         // exchange
		false,
		nil,
	); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to bind notification queue: %w", err)
	}

	service := &QueueService{
		conn:    conn,
		channel: ch,
		config:  cfg,
		logger:  logger,
	}

	logger.Info("Queue service initialized successfully")
	return service, nil
}

// PublishMessage публикует сообщение в очередь
func (s *QueueService) PublishMessage(routingKey string, message *QueueMessage) error {
	// Генерация ID если не указан
	if message.ID == "" {
		message.ID = generateMessageID()
	}

	// Установка временной метки
	if message.Timestamp.IsZero() {
		message.Timestamp = time.Now()
	}

	// Сериализация сообщения
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Публикация сообщения
	err = s.channel.Publish(
		"task_service", // exchange
		routingKey,     // routing key
		false,          // mandatory
		false,          // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         body,
			DeliveryMode: amqp.Persistent,
			Timestamp:    time.Now(),
		},
	)
	if err != nil {
		s.logger.Error("Failed to publish message", zap.String("routing_key", routingKey), zap.Error(err))
		return fmt.Errorf("failed to publish message: %w", err)
	}

	s.logger.Info("Message published successfully",
		zap.String("routing_key", routingKey),
		zap.String("message_id", message.ID))

	return nil
}

// PublishMeetingMessage публикует сообщение о встрече
func (s *QueueService) PublishMeetingMessage(messageType string, data map[string]interface{}) error {
	message := &QueueMessage{
		Type:      messageType,
		Data:      data,
		Timestamp: time.Now(),
	}

	return s.PublishMessage("meeting."+messageType, message)
}

// PublishImportantDateMessage публикует сообщение о важной дате
func (s *QueueService) PublishImportantDateMessage(messageType string, data map[string]interface{}) error {
	message := &QueueMessage{
		Type:      messageType,
		Data:      data,
		Timestamp: time.Now(),
	}

	return s.PublishMessage("important_date."+messageType, message)
}

// PublishIntegrationMessage публикует сообщение об интеграции
func (s *QueueService) PublishIntegrationMessage(messageType string, data map[string]interface{}) error {
	message := &QueueMessage{
		Type:      messageType,
		Data:      data,
		Timestamp: time.Now(),
	}

	return s.PublishMessage("integration."+messageType, message)
}

// PublishContactMessage публикует сообщение о контакте
func (s *QueueService) PublishContactMessage(messageType string, data map[string]interface{}) error {
	message := &QueueMessage{
		Type:      messageType,
		Data:      data,
		Timestamp: time.Now(),
	}

	return s.PublishMessage("contact."+messageType, message)
}

// PublishNotificationMessage публикует сообщение об уведомлении
func (s *QueueService) PublishNotificationMessage(messageType string, data map[string]interface{}) error {
	message := &QueueMessage{
		Type:      messageType,
		Data:      data,
		Timestamp: time.Now(),
	}

	return s.PublishMessage("notification."+messageType, message)
}

// ConsumeMessages начинает потребление сообщений из очереди
func (s *QueueService) ConsumeMessages(queueName string, handler func(*QueueMessage) error) error {
	msgs, err := s.channel.Consume(
		queueName, // queue
		"",        // consumer
		false,     // auto-ack
		false,     // exclusive
		false,     // no-local
		false,     // no-wait
		nil,       // args
	)
	if err != nil {
		return fmt.Errorf("failed to start consuming: %w", err)
	}

	go func() {
		for msg := range msgs {
			var queueMessage QueueMessage
			if err := json.Unmarshal(msg.Body, &queueMessage); err != nil {
				s.logger.Error("Failed to unmarshal message", zap.Error(err))
				msg.Nack(false, false)
				continue
			}

			s.logger.Info("Processing message",
				zap.String("message_id", queueMessage.ID),
				zap.String("type", queueMessage.Type))

			// Обработка сообщения
			if err := handler(&queueMessage); err != nil {
				s.logger.Error("Failed to process message",
					zap.String("message_id", queueMessage.ID),
					zap.Error(err))

				// Повторная попытка если не превышен лимит
				if queueMessage.RetryCount < s.config.RabbitMQ.MaxRetries {
					queueMessage.RetryCount++
					// Отправка в очередь повторных попыток
					s.publishRetryMessage(&queueMessage)
					msg.Ack(false)
				} else {
					// Сообщение отправляется в очередь мертвых писем
					s.publishDeadLetterMessage(&queueMessage)
					msg.Ack(false)
				}
			} else {
				msg.Ack(false)
				s.logger.Info("Message processed successfully",
					zap.String("message_id", queueMessage.ID))
			}
		}
	}()

	s.logger.Info("Started consuming messages from queue", zap.String("queue", queueName))
	return nil
}

// publishRetryMessage отправляет сообщение в очередь повторных попыток
func (s *QueueService) publishRetryMessage(message *QueueMessage) {
	// Задержка перед повторной попыткой
	delay := time.Duration(message.RetryCount) * time.Minute

	// В реальности здесь будет использоваться плагин delayed message exchange
	// Пока просто логируем
	s.logger.Info("Scheduling retry message",
		zap.String("message_id", message.ID),
		zap.Duration("delay", delay))
}

// publishDeadLetterMessage отправляет сообщение в очередь мертвых писем
func (s *QueueService) publishDeadLetterMessage(message *QueueMessage) {
	s.logger.Warn("Message sent to dead letter queue",
		zap.String("message_id", message.ID),
		zap.Int("retry_count", message.RetryCount))
}

// Close закрывает соединение с RabbitMQ
func (s *QueueService) Close() error {
	if err := s.channel.Close(); err != nil {
		s.logger.Error("Failed to close channel", zap.Error(err))
	}

	if err := s.conn.Close(); err != nil {
		s.logger.Error("Failed to close connection", zap.Error(err))
	}

	s.logger.Info("Queue service closed")
	return nil
}

// GetQueueStats получает статистику очередей
func (s *QueueService) GetQueueStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Получение статистики по каждой очереди
	queues := []string{"meeting_queue", "important_date_queue", "integration_queue", "contact_queue", "notification_queue"}

	for _, queueName := range queues {
		queue, err := s.channel.QueueInspect(queueName)
		if err != nil {
			s.logger.Error("Failed to inspect queue", zap.String("queue", queueName), zap.Error(err))
			continue
		}

		stats[queueName] = map[string]interface{}{
			"messages":  queue.Messages,
			"consumers": queue.Consumers,
		}
	}

	return stats, nil
}

// generateMessageID генерирует уникальный ID для сообщения
func generateMessageID() string {
	return fmt.Sprintf("msg_%d", time.Now().UnixNano())
}
