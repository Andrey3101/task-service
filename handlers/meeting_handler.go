package handlers

import (
	"net/http"
	"time"

	"github.com/andre/task-service/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// MeetingHandler обработчик для встреч
type MeetingHandler struct {
	logger *zap.Logger
}

// NewMeetingHandler создает новый экземпляр MeetingHandler
func NewMeetingHandler(logger *zap.Logger) *MeetingHandler {
	return &MeetingHandler{
		logger: logger,
	}
}

// CreateMeeting godoc
// @Summary Create a new meeting
// @Description Create a meeting with participants and scheduling
// @Tags meetings
// @Accept json
// @Produce json
// @Param meeting body models.Meeting true "Meeting data"
// @Success 201 {object} models.Meeting
// @Failure 400 {object} map[string]string "error response"
// @Failure 403 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/meetings [post]
func (h *MeetingHandler) CreateMeeting(c *gin.Context) {
	var meeting models.Meeting
	if err := c.ShouldBindJSON(&meeting); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input: " + err.Error()})
		return
	}

	// Валидация обязательных полей
	if meeting.Title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Title is required"})
		return
	}

	if meeting.StartTime.IsZero() || meeting.EndTime.IsZero() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Start time and end time are required"})
		return
	}

	if meeting.StartTime.After(meeting.EndTime) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Start time must be before end time"})
		return
	}

	// Установка значений по умолчанию
	meeting.ID = uuid.New()
	meeting.UserID = c.GetString("user_id")
	meeting.CompanyID = uuid.MustParse(c.GetString("company_id"))
	meeting.Status = "scheduled"
	meeting.CreatedAt = time.Now()
	meeting.UpdatedAt = time.Now()

	// Здесь должна быть логика сохранения в базу данных
	// Пока возвращаем созданную встречу
	c.JSON(http.StatusCreated, meeting)
}

// GetMeetings godoc
// @Summary Get meetings list
// @Description Get list of meetings with optional filtering
// @Tags meetings
// @Accept json
// @Produce json
// @Param start_date query string false "Start date filter (YYYY-MM-DD)"
// @Param end_date query string false "End date filter (YYYY-MM-DD)"
// @Param meeting_type query string false "Meeting type filter"
// @Param status query string false "Status filter"
// @Success 200 {array} models.Meeting
// @Failure 400 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/meetings [get]
func (h *MeetingHandler) GetMeetings(c *gin.Context) {
	userID := c.GetString("user_id")
	companyID := c.GetString("company_id")

	// Получение параметров фильтрации (пока не используются, но будут для фильтрации)
	_ = c.Query("start_date")
	_ = c.Query("end_date")
	_ = c.Query("meeting_type")
	_ = c.Query("status")

	// Здесь должна быть логика получения встреч из базы данных
	// Пока возвращаем моковые данные
	meetings := []models.Meeting{
		{
			ID:           uuid.New(),
			UserID:       userID,
			CompanyID:    uuid.MustParse(companyID),
			Title:        "Встреча с клиентом",
			Description:  "Обсуждение проекта",
			StartTime:    time.Now().Add(24 * time.Hour),
			EndTime:      time.Now().Add(25 * time.Hour),
			Location:     "Конференц-зал",
			MeetingType:  "client",
			Status:       "scheduled",
			Priority:     "high",
			Participants: []string{"user1", "user2"},
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
	}

	c.JSON(http.StatusOK, meetings)
}

// GetMeeting godoc
// @Summary Get meeting by ID
// @Description Get detailed information about a specific meeting
// @Tags meetings
// @Accept json
// @Produce json
// @Param id path string true "Meeting ID"
// @Success 200 {object} models.Meeting
// @Failure 400 {object} map[string]string "error response"
// @Failure 404 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/meetings/{id} [get]
func (h *MeetingHandler) GetMeeting(c *gin.Context) {
	meetingID := c.Param("id")
	userID := c.GetString("user_id")

	// Валидация UUID
	if _, err := uuid.Parse(meetingID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid meeting ID format"})
		return
	}

	// Здесь должна быть логика получения встречи из базы данных
	// Пока возвращаем моковые данные
	meeting := models.Meeting{
		ID:           uuid.MustParse(meetingID),
		UserID:       userID,
		CompanyID:    uuid.MustParse(c.GetString("company_id")),
		Title:        "Встреча с клиентом",
		Description:  "Обсуждение проекта",
		StartTime:    time.Now().Add(24 * time.Hour),
		EndTime:      time.Now().Add(25 * time.Hour),
		Location:     "Конференц-зал",
		MeetingType:  "client",
		Status:       "scheduled",
		Priority:     "high",
		Participants: []string{"user1", "user2"},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	c.JSON(http.StatusOK, meeting)
}

// UpdateMeeting godoc
// @Summary Update meeting
// @Description Update meeting information
// @Tags meetings
// @Accept json
// @Produce json
// @Param id path string true "Meeting ID"
// @Param meeting body models.Meeting true "Updated meeting data"
// @Success 200 {object} models.Meeting
// @Failure 400 {object} map[string]string "error response"
// @Failure 404 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/meetings/{id} [put]
func (h *MeetingHandler) UpdateMeeting(c *gin.Context) {
	meetingID := c.Param("id")
	userID := c.GetString("user_id")

	// Валидация UUID
	if _, err := uuid.Parse(meetingID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid meeting ID format"})
		return
	}

	var meeting models.Meeting
	if err := c.ShouldBindJSON(&meeting); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input: " + err.Error()})
		return
	}

	// Валидация обязательных полей
	if meeting.Title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Title is required"})
		return
	}

	if meeting.StartTime.After(meeting.EndTime) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Start time must be before end time"})
		return
	}

	// Обновление полей
	meeting.ID = uuid.MustParse(meetingID)
	meeting.UserID = userID
	meeting.CompanyID = uuid.MustParse(c.GetString("company_id"))
	meeting.UpdatedAt = time.Now()

	// Здесь должна быть логика обновления в базе данных
	// Пока возвращаем обновленную встречу
	c.JSON(http.StatusOK, meeting)
}

// DeleteMeeting godoc
// @Summary Delete meeting
// @Description Delete a meeting by ID
// @Tags meetings
// @Accept json
// @Produce json
// @Param id path string true "Meeting ID"
// @Success 200 {object} map[string]string "success response"
// @Failure 400 {object} map[string]string "error response"
// @Failure 404 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/meetings/{id} [delete]
func (h *MeetingHandler) DeleteMeeting(c *gin.Context) {
	meetingID := c.Param("id")

	// Валидация UUID
	if _, err := uuid.Parse(meetingID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid meeting ID format"})
		return
	}

	// Здесь должна быть логика удаления из базы данных
	// Пока возвращаем успешный ответ
	c.JSON(http.StatusOK, gin.H{"message": "Meeting deleted successfully"})
}
