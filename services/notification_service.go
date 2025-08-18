package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/andre/task-service/config"
	"go.uber.org/zap"
)

// NotificationData структура для отправки уведомлений
type NotificationData struct {
	UserID      string                 `json:"user_id"`
	CompanyID   string                 `json:"company_id"`
	Type        string                 `json:"type"`
	Title       string                 `json:"title"`
	Message     string                 `json:"message"`
	Priority    string                 `json:"priority"`
	Channels    []string               `json:"channels"`
	Data        map[string]interface{} `json:"data,omitempty"`
	ScheduledAt *time.Time             `json:"scheduled_at,omitempty"`
}

// NotificationResponse ответ от notification-service
type NotificationResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	ID      string `json:"id,omitempty"`
}

// NotificationService сервис для работы с уведомлениями
type NotificationService struct {
	config     *config.Config
	httpClient *http.Client
	logger     *zap.Logger
	baseURL    string
}

// NewNotificationService создает новый сервис уведомлений
func NewNotificationService(cfg *config.Config, logger *zap.Logger) *NotificationService {
	return &NotificationService{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger:  logger,
		baseURL: cfg.NotificationService.URL,
	}
}

// SendNotification отправляет уведомление
func (s *NotificationService) SendNotification(ctx context.Context, data *NotificationData) (*NotificationResponse, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal notification data: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.baseURL+"/notifications", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.config.NotificationService.APIKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send notification request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("notification service returned status: %d", resp.StatusCode)
	}

	var response NotificationResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	s.logger.Info("Notification sent successfully",
		zap.String("user_id", data.UserID),
		zap.String("type", data.Type),
		zap.String("title", data.Title))

	return &response, nil
}

// SendMeetingNotification отправляет уведомление о встрече
func (s *NotificationService) SendMeetingNotification(ctx context.Context, userID, companyID, title, message string, priority string, channels []string) error {
	data := &NotificationData{
		UserID:    userID,
		CompanyID: companyID,
		Type:      "meeting",
		Title:     title,
		Message:   message,
		Priority:  priority,
		Channels:  channels,
		Data: map[string]interface{}{
			"category": "meeting",
		},
	}

	_, err := s.SendNotification(ctx, data)
	return err
}

// SendImportantDateNotification отправляет уведомление о важной дате
func (s *NotificationService) SendImportantDateNotification(ctx context.Context, userID, companyID, title, message string, priority string, channels []string, scheduledAt *time.Time) error {
	data := &NotificationData{
		UserID:      userID,
		CompanyID:   companyID,
		Type:        "important_date",
		Title:       title,
		Message:     message,
		Priority:    priority,
		Channels:    channels,
		ScheduledAt: scheduledAt,
		Data: map[string]interface{}{
			"category": "important_date",
		},
	}

	_, err := s.SendNotification(ctx, data)
	return err
}

// SendIntegrationNotification отправляет уведомление об интеграции
func (s *NotificationService) SendIntegrationNotification(ctx context.Context, userID, companyID, title, message string, priority string, channels []string) error {
	data := &NotificationData{
		UserID:    userID,
		CompanyID: companyID,
		Type:      "integration",
		Title:     title,
		Message:   message,
		Priority:  priority,
		Channels:  channels,
		Data: map[string]interface{}{
			"category": "integration",
		},
	}

	_, err := s.SendNotification(ctx, data)
	return err
}

// SendContactNotification отправляет уведомление о контакте
func (s *NotificationService) SendContactNotification(ctx context.Context, userID, companyID, title, message string, priority string, channels []string) error {
	data := &NotificationData{
		UserID:    userID,
		CompanyID: companyID,
		Type:      "contact",
		Title:     title,
		Message:   message,
		Priority:  priority,
		Channels:  channels,
		Data: map[string]interface{}{
			"category": "contact",
		},
	}

	_, err := s.SendNotification(ctx, data)
	return err
}

// SendBulkNotifications отправляет массовые уведомления
func (s *NotificationService) SendBulkNotifications(ctx context.Context, notifications []*NotificationData) error {
	for _, notification := range notifications {
		if _, err := s.SendNotification(ctx, notification); err != nil {
			s.logger.Error("Failed to send bulk notification",
				zap.String("user_id", notification.UserID),
				zap.String("type", notification.Type),
				zap.Error(err))
			// Продолжаем отправку остальных уведомлений
		}
	}
	return nil
}

// GetNotificationStatus получает статус уведомления
func (s *NotificationService) GetNotificationStatus(ctx context.Context, notificationID string) (*NotificationResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.baseURL+"/notifications/"+notificationID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.config.NotificationService.APIKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get notification status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("notification service returned status: %d", resp.StatusCode)
	}

	var response NotificationResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response, nil
}

// CancelScheduledNotification отменяет запланированное уведомление
func (s *NotificationService) CancelScheduledNotification(ctx context.Context, notificationID string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", s.baseURL+"/notifications/"+notificationID, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.config.NotificationService.APIKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to cancel notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("notification service returned status: %d", resp.StatusCode)
	}

	s.logger.Info("Scheduled notification cancelled", zap.String("notification_id", notificationID))
	return nil
}
