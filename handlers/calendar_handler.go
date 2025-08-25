package handlers

import (
	"net/http"
	"time"

	"github.com/andre/task-service/models"
	"github.com/andre/task-service/services"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// CalendarHandler обработчик для календаря
type CalendarHandler struct {
	calendarService *services.CalendarService
	logger          *zap.Logger
}

// NewCalendarHandler создает новый экземпляр CalendarHandler
func NewCalendarHandler(calendarService *services.CalendarService, logger *zap.Logger) *CalendarHandler {
	return &CalendarHandler{
		calendarService: calendarService,
		logger:          logger,
	}
}

// CreateEvent создает новое событие в календаре
func (h *CalendarHandler) CreateEvent(c *gin.Context) {
	var event models.CalendarEvent
	if err := c.ShouldBindJSON(&event); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Получаем user_id из контекста (после аутентификации)
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}
	event.UserID = userID.(string)

	// Получаем company_id из контекста
	companyID, exists := c.Get("company_id")
	if !exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Company ID not found"})
		return
	}
	event.CompanyID = companyID.(uuid.UUID)

	if err := h.calendarService.CreateEvent(c.Request.Context(), &event); err != nil {
		h.logger.Error("Failed to create calendar event", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create event"})
		return
	}

	c.JSON(http.StatusCreated, event)
}

// GetEvent получает событие по ID
func (h *CalendarHandler) GetEvent(c *gin.Context) {
	eventIDStr := c.Param("id")
	eventID, err := uuid.Parse(eventIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid event ID"})
		return
	}

	event, err := h.calendarService.GetEventByID(c.Request.Context(), eventID)
	if err != nil {
		h.logger.Error("Failed to get calendar event", zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Event not found"})
		return
	}

	// Проверяем права доступа
	userID, exists := c.Get("user_id")
	if !exists || event.UserID != userID.(string) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	c.JSON(http.StatusOK, event)
}

// GetUserEvents получает события пользователя за период
func (h *CalendarHandler) GetUserEvents(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	// Парсим параметры дат
	startDateStr := c.Query("start_date")
	endDateStr := c.Query("end_date")

	var startDate, endDate time.Time
	var err error

	if startDateStr != "" {
		startDate, err = time.Parse("2006-01-02", startDateStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid start_date format. Use YYYY-MM-DD"})
			return
		}
	} else {
		startDate = time.Now().Truncate(24 * time.Hour)
	}

	if endDateStr != "" {
		endDate, err = time.Parse("2006-01-02", endDateStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid end_date format. Use YYYY-MM-DD"})
			return
		}
	} else {
		endDate = startDate.AddDate(0, 1, 0) // По умолчанию месяц
	}

	events, err := h.calendarService.GetUserEvents(c.Request.Context(), userID.(string), startDate, endDate)
	if err != nil {
		h.logger.Error("Failed to get user events", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get events"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"events": events})
}

// UpdateEvent обновляет событие
func (h *CalendarHandler) UpdateEvent(c *gin.Context) {
	eventIDStr := c.Param("id")
	eventID, err := uuid.Parse(eventIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid event ID"})
		return
	}

	var updateData models.CalendarEvent
	if err := c.ShouldBindJSON(&updateData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Получаем существующее событие
	existingEvent, err := h.calendarService.GetEventByID(c.Request.Context(), eventID)
	if err != nil {
		h.logger.Error("Failed to get event for update", zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Event not found"})
		return
	}

	// Проверяем права доступа
	userID, exists := c.Get("user_id")
	if !exists || existingEvent.UserID != userID.(string) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	// Обновляем только разрешенные поля
	if updateData.Title != "" {
		existingEvent.Title = updateData.Title
	}
	if updateData.Description != "" {
		existingEvent.Description = updateData.Description
	}
	if !updateData.StartTime.IsZero() {
		existingEvent.StartTime = updateData.StartTime
	}
	if !updateData.EndTime.IsZero() {
		existingEvent.EndTime = updateData.EndTime
	}
	if updateData.Location != "" {
		existingEvent.Location = updateData.Location
	}
	if updateData.Priority != "" {
		existingEvent.Priority = updateData.Priority
	}
	if updateData.Status != "" {
		existingEvent.Status = updateData.Status
	}
	if updateData.Color != "" {
		existingEvent.Color = updateData.Color
	}
	existingEvent.IsAllDay = updateData.IsAllDay
	existingEvent.Recurring = updateData.Recurring
	existingEvent.RecurringEnd = updateData.RecurringEnd

	if err := h.calendarService.UpdateEvent(c.Request.Context(), existingEvent); err != nil {
		h.logger.Error("Failed to update calendar event", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update event"})
		return
	}

	c.JSON(http.StatusOK, existingEvent)
}

// DeleteEvent удаляет событие
func (h *CalendarHandler) DeleteEvent(c *gin.Context) {
	eventIDStr := c.Param("id")
	eventID, err := uuid.Parse(eventIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid event ID"})
		return
	}

	// Получаем существующее событие для проверки прав
	existingEvent, err := h.calendarService.GetEventByID(c.Request.Context(), eventID)
	if err != nil {
		h.logger.Error("Failed to get event for deletion", zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Event not found"})
		return
	}

	// Проверяем права доступа
	userID, exists := c.Get("user_id")
	if !exists || existingEvent.UserID != userID.(string) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	if err := h.calendarService.DeleteEvent(c.Request.Context(), eventID); err != nil {
		h.logger.Error("Failed to delete calendar event", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete event"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Event deleted successfully"})
}

// GetUserCalendarSettings получает настройки календаря пользователя
func (h *CalendarHandler) GetUserCalendarSettings(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	settings, err := h.calendarService.GetUserCalendarSettings(c.Request.Context(), userID.(string))
	if err != nil {
		h.logger.Error("Failed to get user calendar settings", zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Settings not found"})
		return
	}

	c.JSON(http.StatusOK, settings)
}

// UpdateUserCalendarSettings обновляет настройки календаря пользователя
func (h *CalendarHandler) UpdateUserCalendarSettings(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	var settings models.CalendarSettings
	if err := c.ShouldBindJSON(&settings); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Устанавливаем user_id из контекста
	settings.UserID = userID.(string)

	if err := h.calendarService.UpdateUserCalendarSettings(c.Request.Context(), &settings); err != nil {
		h.logger.Error("Failed to update user calendar settings", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update settings"})
		return
	}

	c.JSON(http.StatusOK, settings)
}

// AddReminder добавляет напоминание к событию
func (h *CalendarHandler) AddReminder(c *gin.Context) {
	eventIDStr := c.Param("id")
	eventID, err := uuid.Parse(eventIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid event ID"})
		return
	}

	var reminder models.CalendarReminder
	if err := c.ShouldBindJSON(&reminder); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Устанавливаем event_id из URL
	reminder.EventID = eventID

	// Проверяем права доступа к событию
	existingEvent, err := h.calendarService.GetEventByID(c.Request.Context(), eventID)
	if err != nil {
		h.logger.Error("Failed to get event for reminder", zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Event not found"})
		return
	}

	userID, exists := c.Get("user_id")
	if !exists || existingEvent.UserID != userID.(string) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	if err := h.calendarService.AddReminder(c.Request.Context(), &reminder); err != nil {
		h.logger.Error("Failed to add reminder", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add reminder"})
		return
	}

	c.JSON(http.StatusCreated, reminder)
}

// GetUserTags получает теги пользователя
func (h *CalendarHandler) GetUserTags(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	tags, err := h.calendarService.GetUserTags(c.Request.Context(), userID.(string))
	if err != nil {
		h.logger.Error("Failed to get user tags", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get tags"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"tags": tags})
}

// CreateTag создает новый тег
func (h *CalendarHandler) CreateTag(c *gin.Context) {
	var tag models.CalendarTag
	if err := c.ShouldBindJSON(&tag); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.calendarService.CreateTag(c.Request.Context(), &tag); err != nil {
		h.logger.Error("Failed to create tag", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create tag"})
		return
	}

	c.JSON(http.StatusCreated, tag)
}

// SyncTasksToCalendar синхронизирует задачи в календарь
func (h *CalendarHandler) SyncTasksToCalendar(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	// Получаем задачи пользователя (здесь нужно будет добавить TaskService)
	// Пока что возвращаем заглушку
	c.JSON(http.StatusOK, gin.H{"message": "Task synchronization initiated", "user_id": userID})
}
