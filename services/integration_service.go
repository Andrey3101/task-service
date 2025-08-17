package services

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/andre/task-service/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type IntegrationService struct {
	db *gorm.DB
}

func NewIntegrationService(db *gorm.DB) *IntegrationService {
	return &IntegrationService{db: db}
}

// CreateExternalIntegration создает новую внешнюю интеграцию
func (s *IntegrationService) CreateExternalIntegration(ctx context.Context, userID string, companyID uuid.UUID, integration *models.ExternalIntegration) (*models.ExternalIntegration, error) {
	// Валидация бизнес-логики
	if err := s.validateIntegration(integration); err != nil {
		return nil, err
	}

	// Проверка уникальности интеграции
	if err := s.checkIntegrationUniqueness(ctx, userID, companyID, integration.Provider, integration.Type); err != nil {
		return nil, err
	}

	// Шифрование учетных данных (в реальности здесь будет криптография)
	encryptedCredentials, err := s.encryptCredentials(integration.Credentials)
	if err != nil {
		return nil, err
	}

	// Установка значений по умолчанию
	integration.ID = uuid.New()
	integration.UserID = userID
	integration.CompanyID = companyID
	integration.Credentials = encryptedCredentials
	integration.IsActive = true
	integration.CreatedAt = time.Now()
	integration.UpdatedAt = time.Now()

	// Сохранение в базу данных
	if err := s.db.Create(integration).Error; err != nil {
		return nil, err
	}

	// Создание лога интеграции
	s.createIntegrationLog(integration.ID, "create", "success", "Integration created successfully", nil)

	// Тестирование подключения
	go s.testIntegrationConnection(integration)

	return integration, nil
}

// GetExternalIntegrations получает список внешних интеграций
func (s *IntegrationService) GetExternalIntegrations(ctx context.Context, userID string, companyID uuid.UUID, filters map[string]interface{}) ([]models.ExternalIntegration, error) {
	var integrations []models.ExternalIntegration
	query := s.db.Where("company_id = ?", companyID)

	// Применение фильтров
	if provider, ok := filters["provider"].(string); ok {
		query = query.Where("provider = ?", provider)
	}
	if integrationType, ok := filters["type"].(string); ok {
		query = query.Where("type = ?", integrationType)
	}
	if isActive, ok := filters["is_active"].(bool); ok {
		query = query.Where("is_active = ?", isActive)
	}

	// Получение интеграций пользователя или всех интеграций компании
	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}

	if err := query.Order("created_at DESC").Find(&integrations).Error; err != nil {
		return nil, err
	}

	// Расшифровка учетных данных (в реальности здесь будет криптография)
	for i := range integrations {
		decryptedCredentials, err := s.decryptCredentials(integrations[i].Credentials)
		if err == nil {
			integrations[i].Credentials = decryptedCredentials
		}
	}

	return integrations, nil
}

// GetExternalIntegration получает внешнюю интеграцию по ID
func (s *IntegrationService) GetExternalIntegration(ctx context.Context, integrationID uuid.UUID, userID string, companyID uuid.UUID) (*models.ExternalIntegration, error) {
	var integration models.ExternalIntegration
	if err := s.db.Where("id = ? AND company_id = ?", integrationID, companyID).First(&integration).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("integration not found")
		}
		return nil, err
	}

	// Проверка доступа пользователя к интеграции
	if integration.UserID != userID {
		return nil, errors.New("access denied")
	}

	// Расшифровка учетных данных
	decryptedCredentials, err := s.decryptCredentials(integration.Credentials)
	if err == nil {
		integration.Credentials = decryptedCredentials
	}

	return &integration, nil
}

// UpdateExternalIntegration обновляет внешнюю интеграцию
func (s *IntegrationService) UpdateExternalIntegration(ctx context.Context, integrationID uuid.UUID, userID string, companyID uuid.UUID, updates *models.ExternalIntegration) (*models.ExternalIntegration, error) {
	// Получение существующей интеграции
	existingIntegration, err := s.GetExternalIntegration(ctx, integrationID, userID, companyID)
	if err != nil {
		return nil, err
	}

	// Валидация обновлений
	if err := s.validateIntegration(updates); err != nil {
		return nil, err
	}

	// Шифрование учетных данных если изменились
	if updates.Credentials != existingIntegration.Credentials {
		encryptedCredentials, err := s.encryptCredentials(updates.Credentials)
		if err != nil {
			return nil, err
		}
		updates.Credentials = encryptedCredentials
	}

	// Обновление полей
	updates.ID = integrationID
	updates.UserID = userID
	updates.CompanyID = companyID
	updates.UpdatedAt = time.Now()

	// Сохранение изменений
	if err := s.db.Model(existingIntegration).Updates(updates).Error; err != nil {
		return nil, err
	}

	// Создание лога интеграции
	s.createIntegrationLog(integrationID, "update", "success", "Integration updated successfully", nil)

	// Тестирование подключения если изменились учетные данные
	if updates.Credentials != existingIntegration.Credentials {
		go s.testIntegrationConnection(updates)
	}

	return updates, nil
}

// DeleteExternalIntegration удаляет внешнюю интеграцию
func (s *IntegrationService) DeleteExternalIntegration(ctx context.Context, integrationID uuid.UUID, userID string, companyID uuid.UUID) error {
	// Получение существующей интеграции
	existingIntegration, err := s.GetExternalIntegration(ctx, integrationID, userID, companyID)
	if err != nil {
		return err
	}

	// Удаление логов интеграции
	if err := s.db.Where("integration_id = ?", integrationID).Delete(&models.IntegrationLog{}).Error; err != nil {
		return err
	}

	// Удаление интеграции
	if err := s.db.Delete(existingIntegration).Error; err != nil {
		return err
	}

	// Создание лога интеграции
	s.createIntegrationLog(integrationID, "delete", "success", "Integration deleted successfully", nil)

	return nil
}

// SyncExternalData синхронизирует данные с внешней системой
func (s *IntegrationService) SyncExternalData(ctx context.Context, integrationID uuid.UUID, userID string, companyID uuid.UUID) error {
	// Получение интеграции
	integration, err := s.GetExternalIntegration(ctx, integrationID, userID, companyID)
	if err != nil {
		return err
	}

	// Проверка активности интеграции
	if !integration.IsActive {
		return errors.New("integration is not active")
	}

	// Начало синхронизации
	startTime := time.Now()
	s.createIntegrationLog(integrationID, "sync", "warning", "Sync started", nil)

	// Выполнение синхронизации в зависимости от типа
	var syncResult map[string]interface{}
	var syncError error

	switch integration.Type {
	case "calendar":
		syncResult, syncError = s.syncCalendarData(integration)
	case "messaging":
		syncResult, syncError = s.syncMessagingData(integration)
	case "notification":
		syncResult, syncError = s.syncNotificationData(integration)
	case "crm":
		syncResult, syncError = s.syncCRMData(integration)
	default:
		syncError = errors.New("unsupported integration type")
	}

	// Обработка результата синхронизации
	if syncError != nil {
		s.createIntegrationLog(integrationID, "sync", "error", "Sync failed: "+syncError.Error(), nil)
		return syncError
	}

	// Обновление времени последней синхронизации
	duration := time.Since(startTime)
	if err := s.db.Model(integration).Update("last_sync", time.Now()).Error; err != nil {
		return err
	}

	// Создание лога успешной синхронизации
	s.createIntegrationLog(integrationID, "sync", "success", "Sync completed successfully", syncResult)

	return nil
}

// TestIntegrationConnection тестирует подключение к внешней системе
func (s *IntegrationService) TestIntegrationConnection(integration *models.ExternalIntegration) error {
	// Создание лога тестирования
	s.createIntegrationLog(integration.ID, "test_connection", "warning", "Testing connection", nil)

	// Тестирование подключения в зависимости от провайдера
	var testError error
	switch integration.Provider {
	case "google_calendar":
		testError = s.testGoogleCalendarConnection(integration)
	case "outlook":
		testError = s.testOutlookConnection(integration)
	case "slack":
		testError = s.testSlackConnection(integration)
	case "telegram":
		testError = s.testTelegramConnection(integration)
	default:
		testError = errors.New("unsupported provider")
	}

	// Обработка результата тестирования
	if testError != nil {
		s.createIntegrationLog(integration.ID, "test_connection", "error", "Connection test failed: "+testError.Error(), nil)
		return testError
	}

	// Создание лога успешного тестирования
	s.createIntegrationLog(integration.ID, "test_connection", "success", "Connection test successful", nil)
	return nil
}

// GetIntegrationLogs получает логи интеграции
func (s *IntegrationService) GetIntegrationLogs(ctx context.Context, integrationID uuid.UUID, userID string, companyID uuid.UUID, limit int) ([]models.IntegrationLog, error) {
	var logs []models.IntegrationLog

	// Проверка доступа к интеграции
	if _, err := s.GetExternalIntegration(ctx, integrationID, userID, companyID); err != nil {
		return nil, err
	}

	// Получение логов
	query := s.db.Where("integration_id = ?", integrationID)
	if limit > 0 {
		query = query.Limit(limit)
	}

	if err := query.Order("created_at DESC").Find(&logs).Error; err != nil {
		return nil, err
	}

	return logs, nil
}

// Вспомогательные методы

func (s *IntegrationService) validateIntegration(integration *models.ExternalIntegration) error {
	if integration.Provider == "" {
		return errors.New("provider is required")
	}
	if integration.Type == "" {
		return errors.New("type is required")
	}
	if integration.Credentials == "" {
		return errors.New("credentials are required")
	}

	// Валидация провайдера
	validProviders := []string{"google_calendar", "outlook", "slack", "telegram", "whatsapp"}
	valid := false
	for _, validProvider := range validProviders {
		if integration.Provider == validProvider {
			valid = true
			break
		}
	}
	if !valid {
		return errors.New("invalid provider")
	}

	// Валидация типа
	validTypes := []string{"calendar", "messaging", "notification", "crm"}
	valid = false
	for _, validType := range validTypes {
		if integration.Type == validType {
			valid = true
			break
		}
	}
	if !valid {
		return errors.New("invalid type")
	}

	return nil
}

func (s *IntegrationService) checkIntegrationUniqueness(ctx context.Context, userID string, companyID uuid.UUID, provider string, integrationType string) error {
	var count int64
	err := s.db.Model(&models.ExternalIntegration{}).
		Where("user_id = ? AND company_id = ? AND provider = ? AND type = ?", userID, companyID, provider, integrationType).
		Count(&count).Error

	if err != nil {
		return err
	}

	if count > 0 {
		return errors.New("integration with this provider and type already exists")
	}

	return nil
}

func (s *IntegrationService) createIntegrationLog(integrationID uuid.UUID, action string, status string, message string, details map[string]interface{}) {
	var detailsJSON string
	if details != nil {
		if jsonData, err := json.Marshal(details); err == nil {
			detailsJSON = string(jsonData)
		}
	}

	log := models.IntegrationLog{
		ID:            uuid.New(),
		IntegrationID: integrationID,
		Action:        action,
		Status:        status,
		Message:       message,
		Details:       detailsJSON,
		CreatedAt:     time.Now(),
	}

	s.db.Create(&log)
}

// Методы для синхронизации (заглушки для демонстрации)
func (s *IntegrationService) syncCalendarData(integration *models.ExternalIntegration) (map[string]interface{}, error) {
	// Логика синхронизации календаря
	return map[string]interface{}{
		"events_synced":  15,
		"events_created": 3,
		"events_updated": 12,
	}, nil
}

func (s *IntegrationService) syncMessagingData(integration *models.ExternalIntegration) (map[string]interface{}, error) {
	// Логика синхронизации сообщений
	return map[string]interface{}{
		"messages_synced": 25,
		"channels_synced": 2,
	}, nil
}

func (s *IntegrationService) syncNotificationData(integration *models.ExternalIntegration) (map[string]interface{}, error) {
	// Логика синхронизации уведомлений
	return map[string]interface{}{
		"notifications_synced": 10,
		"templates_synced":     5,
	}, nil
}

func (s *IntegrationService) syncCRMData(integration *models.ExternalIntegration) (map[string]interface{}, error) {
	// Логика синхронизации CRM данных
	return map[string]interface{}{
		"contacts_synced": 50,
		"deals_synced":    8,
		"tasks_synced":    12,
	}, nil
}

// Методы для тестирования подключений (заглушки для демонстрации)
func (s *IntegrationService) testGoogleCalendarConnection(integration *models.ExternalIntegration) error {
	// Логика тестирования подключения к Google Calendar
	return nil
}

func (s *IntegrationService) testOutlookConnection(integration *models.ExternalIntegration) error {
	// Логика тестирования подключения к Outlook
	return nil
}

func (s *IntegrationService) testSlackConnection(integration *models.ExternalIntegration) error {
	// Логика тестирования подключения к Slack
	return nil
}

func (s *IntegrationService) testTelegramConnection(integration *models.ExternalIntegration) error {
	// Логика тестирования подключения к Telegram
	return nil
}

// Методы для шифрования/расшифровки (заглушки для демонстрации)
func (s *IntegrationService) encryptCredentials(credentials string) (string, error) {
	// В реальности здесь будет криптография
	// Пока просто возвращаем как есть
	return credentials, nil
}

func (s *IntegrationService) decryptCredentials(encryptedCredentials string) (string, error) {
	// В реальности здесь будет криптография
	// Пока просто возвращаем как есть
	return encryptedCredentials, nil
}
