package handlers

import (
	"net/http"
	"time"

	"github.com/andre/task-service/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

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
func (h *TaskHandler) CreateExternalIntegration(c *gin.Context) {
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
func (h *TaskHandler) GetExternalIntegrations(c *gin.Context) {
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
func (h *TaskHandler) GetExternalIntegration(c *gin.Context) {
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
func (h *TaskHandler) UpdateExternalIntegration(c *gin.Context) {
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
func (h *TaskHandler) DeleteExternalIntegration(c *gin.Context) {
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
func (h *TaskHandler) SyncExternalData(c *gin.Context) {
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
