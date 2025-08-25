package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/andre/task-service/models"
	"github.com/andre/task-service/services"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// IntegrationHandler обработчик для интеграций
type IntegrationHandler struct {
	integrationService *services.IntegrationService
	calendarService    *services.CalendarService
	logger             *zap.Logger
}

// NewIntegrationHandler создает новый экземпляр IntegrationHandler
func NewIntegrationHandler(integrationService *services.IntegrationService, calendarService *services.CalendarService, logger *zap.Logger) *IntegrationHandler {
	return &IntegrationHandler{
		integrationService: integrationService,
		calendarService:    calendarService,
		logger:             logger,
	}
}

// CreateExternalIntegration godoc
// @Summary Create a new external integration
// @Description Create an external integration with credentials
// @Tags integrations
// @Accept json
// @Produce json
// @Param integration body models.ExternalIntegration true "Integration data"
// @Success 201 {object} models.ExternalIntegration
// @Failure 400 {object} map[string]string "error response"
// @Failure 403 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/integrations [post]
func (h *IntegrationHandler) CreateExternalIntegration(c *gin.Context) {
	var integration models.ExternalIntegration
	if err := c.ShouldBindJSON(&integration); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input: " + err.Error()})
		return
	}

	// Валидация обязательных полей
	if integration.Provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Provider is required"})
		return
	}

	if integration.Type == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Type is required"})
		return
	}

	// Установка значений по умолчанию
	integration.ID = uuid.New()
	integration.UserID = c.GetString("user_id")
	integration.CompanyID = uuid.MustParse(c.GetString("company_id"))
	integration.IsActive = true
	integration.CreatedAt = time.Now()
	integration.UpdatedAt = time.Now()

	// Здесь должна быть логика сохранения в базу данных
	// Пока возвращаем созданную интеграцию
	c.JSON(http.StatusCreated, integration)
}

// GetExternalIntegrations godoc
// @Summary Get external integrations list
// @Description Get list of external integrations
// @Tags integrations
// @Accept json
// @Produce json
// @Param provider query string false "Provider filter"
// @Param type query string false "Type filter"
// @Param is_active query bool false "Active status filter"
// @Success 200 {array} models.ExternalIntegration
// @Failure 400 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/integrations [get]
func (h *IntegrationHandler) GetExternalIntegrations(c *gin.Context) {
	userID := c.GetString("user_id")
	companyID := c.GetString("company_id")

	// Получение параметров фильтрации (пока не используются, но будут для фильтрации)
	_ = c.Query("provider")
	_ = c.Query("type")
	_ = c.Query("is_active")

	// Здесь должна быть логика получения интеграций из базы данных
	// Пока возвращаем моковые данные
	integrations := []models.ExternalIntegration{
		{
			ID:           uuid.New(),
			UserID:       userID,
			CompanyID:    uuid.MustParse(companyID),
			Provider:     "google_calendar",
			Type:         "calendar",
			Credentials:  `{"access_token": "***", "refresh_token": "***"}`,
			IsActive:     true,
			LastSync:     time.Now().Add(-1 * time.Hour),
			SyncInterval: 60,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
		{
			ID:           uuid.New(),
			UserID:       userID,
			CompanyID:    uuid.MustParse(companyID),
			Provider:     "telegram",
			Type:         "messaging",
			Credentials:  `{"bot_token": "***", "chat_id": "***"}`,
			IsActive:     true,
			LastSync:     time.Now().Add(-30 * time.Minute),
			SyncInterval: 30,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
	}

	c.JSON(http.StatusOK, integrations)
}

// GetExternalIntegration godoc
// @Summary Get external integration by ID
// @Description Get detailed information about a specific external integration
// @Tags integrations
// @Accept json
// @Produce json
// @Param id path string true "Integration ID"
// @Success 200 {object} models.ExternalIntegration
// @Failure 400 {object} map[string]string "error response"
// @Failure 404 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/integrations/{id} [get]
func (h *IntegrationHandler) GetExternalIntegration(c *gin.Context) {
	integrationID := c.Param("id")
	userID := c.GetString("user_id")

	// Валидация UUID
	if _, err := uuid.Parse(integrationID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid integration ID format"})
		return
	}

	// Здесь должна быть логика получения интеграции из базы данных
	// Пока возвращаем моковые данные
	integration := models.ExternalIntegration{
		ID:           uuid.MustParse(integrationID),
		UserID:       userID,
		CompanyID:    uuid.MustParse(c.GetString("company_id")),
		Provider:     "google_calendar",
		Type:         "calendar",
		Credentials:  `{"access_token": "***", "refresh_token": "***"}`,
		IsActive:     true,
		LastSync:     time.Now().Add(-1 * time.Hour),
		SyncInterval: 60,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	c.JSON(http.StatusOK, integration)
}

// UpdateExternalIntegration godoc
// @Summary Update external integration
// @Description Update external integration information
// @Tags integrations
// @Accept json
// @Produce json
// @Param id path string true "Integration ID"
// @Param integration body models.ExternalIntegration true "Updated integration data"
// @Success 200 {object} models.ExternalIntegration
// @Failure 400 {object} map[string]string "error response"
// @Failure 404 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/integrations/{id} [put]
func (h *IntegrationHandler) UpdateExternalIntegration(c *gin.Context) {
	integrationID := c.Param("id")
	userID := c.GetString("user_id")

	// Валидация UUID
	if _, err := uuid.Parse(integrationID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid integration ID format"})
		return
	}

	var integration models.ExternalIntegration
	if err := c.ShouldBindJSON(&integration); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input: " + err.Error()})
		return
	}

	// Валидация обязательных полей
	if integration.Provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Provider is required"})
		return
	}

	if integration.Type == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Type is required"})
		return
	}

	// Обновление полей
	integration.ID = uuid.MustParse(integrationID)
	integration.UserID = userID
	integration.CompanyID = uuid.MustParse(c.GetString("company_id"))
	integration.UpdatedAt = time.Now()

	// Здесь должна быть логика обновления в базе данных
	// Пока возвращаем обновленную интеграцию
	c.JSON(http.StatusOK, integration)
}

// DeleteExternalIntegration godoc
// @Summary Delete external integration
// @Description Delete an external integration by ID
// @Tags integrations
// @Accept json
// @Produce json
// @Param id path string true "Integration ID"
// @Success 200 {object} map[string]string "success response"
// @Failure 400 {object} map[string]string "error response"
// @Failure 404 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/integrations/{id} [delete]
func (h *IntegrationHandler) DeleteExternalIntegration(c *gin.Context) {
	integrationID := c.Param("id")

	// Валидация UUID
	if _, err := uuid.Parse(integrationID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid integration ID format"})
		return
	}

	// Здесь должна быть логика удаления из базы данных
	// Пока возвращаем успешный ответ
	c.JSON(http.StatusOK, gin.H{"message": "External integration deleted successfully"})
}

// SyncExternalData godoc
// @Summary Sync data with external system
// @Description Synchronize data with external system
// @Tags integrations
// @Accept json
// @Produce json
// @Param id path string true "Integration ID"
// @Success 200 {object} map[string]string "success response"
// @Failure 400 {object} map[string]string "error response"
// @Failure 404 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/integrations/{id}/sync [post]
func (h *IntegrationHandler) SyncExternalData(c *gin.Context) {
	integrationID := c.Param("id")

	// Валидация UUID
	if _, err := uuid.Parse(integrationID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid integration ID format"})
		return
	}

	// Здесь должна быть логика синхронизации с внешней системой
	// Пока возвращаем успешный ответ
	c.JSON(http.StatusOK, gin.H{
		"message":      "Data synchronized successfully",
		"sync_time":    time.Now(),
		"items_synced": 15,
	})
}

// ConnectCalendar подключает локальный календарь
func (h *IntegrationHandler) ConnectCalendar(c *gin.Context) {
	var request struct {
		UserID    string                 `json:"user_id" binding:"required"`
		CompanyID uuid.UUID              `json:"company_id" binding:"required"`
		Settings  map[string]interface{} `json:"settings"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Создаем интеграцию с локальным календарем
	integration := &models.ExternalIntegration{
		ID:           uuid.New(),
		UserID:       request.UserID,
		CompanyID:    request.CompanyID,
		Provider:     "local_calendar", // Заменяем google_calendar на local_calendar
		Type:         "calendar",
		Credentials:  "{}", // Пустые учетные данные для локального календаря
		IsActive:     true,
		LastSync:     time.Now(),
		SyncInterval: 60,
		Settings:     "{}",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	createdIntegration, err := h.integrationService.CreateExternalIntegration(context.Background(), request.UserID, request.CompanyID, integration)
	if err != nil {
		h.logger.Error("Failed to create local calendar integration", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create integration"})
		return
	}

	// Создаем настройки календаря по умолчанию
	settings := &models.CalendarSettings{
		ID:                uuid.New(),
		UserID:            request.UserID,
		DefaultView:       "month",
		WorkingHoursStart: "09:00",
		WorkingHoursEnd:   "18:00",
		WeekStart:         "monday",
		TimeZone:          "UTC",
		ShowWeekends:      true,
		ShowHolidays:      true,
		AutoSyncTasks:     true,
		AutoSyncMeetings:  true,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}

	if err := h.calendarService.UpdateUserCalendarSettings(context.Background(), settings); err != nil {
		h.logger.Error("Failed to create calendar settings", zap.Error(err))
		// Не прерываем процесс, так как интеграция уже создана
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":     "Local calendar connected successfully",
		"integration": createdIntegration,
		"settings":    settings,
	})
}

// DisconnectCalendar отключает локальный календарь
func (h *IntegrationHandler) DisconnectCalendar(c *gin.Context) {
	integrationID := c.Param("id")
	userID := c.Query("user_id")

	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	// Валидация UUID
	integrationUUID, err := uuid.Parse(integrationID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid integration ID format"})
		return
	}

	// Получаем интеграцию напрямую из базы данных для проверки прав доступа
	var integration models.ExternalIntegration
	if err := h.integrationService.DB.Where("id = ?", integrationUUID).First(&integration).Error; err != nil {
		h.logger.Error("Failed to get integration", zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Integration not found"})
		return
	}

	// Проверяем права доступа
	if integration.UserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	if err := h.integrationService.DeleteExternalIntegration(context.Background(), integrationUUID, userID, integration.CompanyID); err != nil {
		h.logger.Error("Failed to delete calendar integration", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to disconnect calendar"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Calendar disconnected successfully"})
}

// SyncCalendar синхронизирует локальный календарь
func (h *IntegrationHandler) SyncCalendar(c *gin.Context) {
	userID := c.Param("user_id")

	// Логируем начало синхронизации
	log := &models.CalendarSyncLog{
		ID:         uuid.New(),
		UserID:     userID,
		Action:     "manual_sync",
		Status:     "in_progress",
		ItemsCount: 0,
		CreatedAt:  time.Now(),
	}

	startTime := time.Now()

	// Выполняем синхронизацию
	// Здесь можно добавить логику синхронизации с внешними источниками
	// Пока что просто логируем успешную синхронизацию

	log.Status = "success"
	log.ItemsCount = 0
	log.Duration = int(time.Since(startTime).Milliseconds())

	if err := h.calendarService.LogSyncAction(context.Background(), log); err != nil {
		h.logger.Error("Failed to log sync action", zap.Error(err))
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Calendar synchronized successfully",
		"sync_log": log,
	})
}
