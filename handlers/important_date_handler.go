package handlers

import (
	"net/http"
	"time"

	"github.com/andre/task-service/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ImportantDateHandler обработчик для важных дат
type ImportantDateHandler struct {
	logger *zap.Logger
}

// NewImportantDateHandler создает новый экземпляр ImportantDateHandler
func NewImportantDateHandler(logger *zap.Logger) *ImportantDateHandler {
	return &ImportantDateHandler{
		logger: logger,
	}
}

// CreateImportantDate godoc
// @Summary Create a new important date
// @Description Create an important date with reminder settings
// @Tags important-dates
// @Accept json
// @Produce json
// @Param date body models.ImportantDate true "Important date data"
// @Success 201 {object} models.ImportantDate
// @Failure 400 {object} map[string]string "error response"
// @Failure 403 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/important-dates [post]
func (h *ImportantDateHandler) CreateImportantDate(c *gin.Context) {
	var importantDate models.ImportantDate
	if err := c.ShouldBindJSON(&importantDate); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input: " + err.Error()})
		return
	}

	// Валидация обязательных полей
	if importantDate.Title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Title is required"})
		return
	}

	if importantDate.Date.IsZero() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Date is required"})
		return
	}

	// Установка значений по умолчанию
	importantDate.ID = uuid.New()
	importantDate.UserID = c.GetString("user_id")
	importantDate.CompanyID = uuid.MustParse(c.GetString("company_id"))
	importantDate.Type = "other"
	importantDate.Priority = "medium"
	importantDate.ReminderDays = []int{1}
	importantDate.CreatedAt = time.Now()
	importantDate.UpdatedAt = time.Now()

	// Здесь должна быть логика сохранения в базу данных
	// Пока возвращаем созданную важную дату
	c.JSON(http.StatusCreated, importantDate)
}

// GetImportantDates godoc
// @Summary Get important dates list
// @Description Get list of important dates with optional filtering
// @Tags important-dates
// @Accept json
// @Produce json
// @Param start_date query string false "Start date filter (YYYY-MM-DD)"
// @Param end_date query string false "End date filter (YYYY-MM-DD)"
// @Param type query string false "Type filter"
// @Param priority query string false "Priority filter"
// @Success 200 {array} models.ImportantDate
// @Failure 400 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/important-dates [get]
func (h *ImportantDateHandler) GetImportantDates(c *gin.Context) {
	userID := c.GetString("user_id")
	companyID := c.GetString("company_id")

	// Получение параметров фильтрации (пока не используются, но будут для фильтрации)
	_ = c.Query("start_date")
	_ = c.Query("end_date")
	_ = c.Query("type")
	_ = c.Query("priority")

	// Здесь должна быть логика получения важных дат из базы данных
	// Пока возвращаем моковые данные
	importantDates := []models.ImportantDate{
		{
			ID:           uuid.New(),
			UserID:       userID,
			CompanyID:    uuid.MustParse(companyID),
			Title:        "День рождения клиента",
			Description:  "Поздравить клиента с днем рождения",
			Date:         time.Now().AddDate(0, 0, 7),
			Type:         "birthday",
			Priority:     "medium",
			ReminderDays: []int{7, 1},
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
		{
			ID:           uuid.New(),
			UserID:       userID,
			CompanyID:    uuid.MustParse(companyID),
			Title:        "Срок подачи налоговой декларации",
			Description:  "Ежеквартальная отчетность",
			Date:         time.Now().AddDate(0, 1, 0),
			Type:         "tax_deadline",
			Priority:     "high",
			ReminderDays: []int{7, 3, 1},
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
	}

	c.JSON(http.StatusOK, importantDates)
}

// GetImportantDate godoc
// @Summary Get important date by ID
// @Description Get important date details by ID
// @Tags important-dates
// @Accept json
// @Produce json
// @Param id path string true "Important date ID"
// @Success 200 {object} models.ImportantDate
// @Failure 400 {object} map[string]string "error response"
// @Failure 404 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/important-dates/{id} [get]
func (h *ImportantDateHandler) GetImportantDate(c *gin.Context) {
	dateID := c.Param("id")
	userID := c.GetString("user_id")

	// Здесь должна быть логика получения важной даты из базы данных
	// Пока возвращаем моковые данные
	date := models.ImportantDate{
		ID:           uuid.MustParse(dateID),
		UserID:       userID,
		CompanyID:    uuid.MustParse(c.GetString("company_id")),
		Title:        "Срок подачи налоговой декларации",
		Description:  "Ежеквартальная отчетность",
		Date:         time.Now().AddDate(0, 1, 0),
		Type:         "tax_deadline",
		Priority:     "high",
		ReminderDays: []int{7, 3, 1},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	c.JSON(http.StatusOK, date)
}

// UpdateImportantDate godoc
// @Summary Update important date
// @Description Update important date details
// @Tags important-dates
// @Accept json
// @Produce json
// @Param id path string true "Important date ID"
// @Param date body models.ImportantDate true "Updated important date data"
// @Success 200 {object} models.ImportantDate
// @Failure 400 {object} map[string]string "error response"
// @Failure 403 {object} map[string]string "error response"
// @Failure 404 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/important-dates/{id} [put]
func (h *ImportantDateHandler) UpdateImportantDate(c *gin.Context) {
	dateID := c.Param("id")
	userID := c.GetString("user_id")

	// Валидация существования важной даты
	if dateID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Date ID is required"})
		return
	}

	var importantDate models.ImportantDate
	if err := c.ShouldBindJSON(&importantDate); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input: " + err.Error()})
		return
	}

	// Валидация обязательных полей
	if importantDate.Title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Title is required"})
		return
	}

	if importantDate.Date.IsZero() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Date is required"})
		return
	}

	// Обновление полей
	importantDate.ID = uuid.MustParse(dateID)
	importantDate.UserID = userID
	importantDate.CompanyID = uuid.MustParse(c.GetString("company_id"))
	importantDate.UpdatedAt = time.Now()

	// Здесь должна быть логика обновления в базе данных
	// Пока возвращаем обновленную дату
	c.JSON(http.StatusOK, importantDate)
}

// DeleteImportantDate godoc
// @Summary Delete important date
// @Description Delete an important date by ID
// @Tags important-dates
// @Accept json
// @Produce json
// @Param id path string true "Important date ID"
// @Success 200 {object} map[string]string "success response"
// @Failure 400 {object} map[string]string "error response"
// @Failure 404 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/important-dates/{id} [delete]
func (h *ImportantDateHandler) DeleteImportantDate(c *gin.Context) {
	dateID := c.Param("id")

	// Валидация UUID
	if _, err := uuid.Parse(dateID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date ID format"})
		return
	}

	// Здесь должна быть логика удаления из базы данных
	// Пока возвращаем успешный ответ
	c.JSON(http.StatusOK, gin.H{"message": "Important date deleted successfully"})
}
