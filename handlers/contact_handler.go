package handlers

import (
	"net/http"
	"time"

	"github.com/andre/task-service/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ContactHandler обработчик для контактов
type ContactHandler struct {
	logger *zap.Logger
}

// NewContactHandler создает новый экземпляр ContactHandler
func NewContactHandler(logger *zap.Logger) *ContactHandler {
	return &ContactHandler{
		logger: logger,
	}
}

// CreateContact godoc
// @Summary Create a new contact
// @Description Create a contact with personal and business information
// @Tags contacts
// @Accept json
// @Produce json
// @Param contact body models.Contact true "Contact data"
// @Success 201 {object} models.Contact
// @Failure 400 {object} map[string]string "error response"
// @Failure 403 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/contacts [post]
func (h *ContactHandler) CreateContact(c *gin.Context) {
	var contact models.Contact
	if err := c.ShouldBindJSON(&contact); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input: " + err.Error()})
		return
	}

	// Валидация обязательных полей
	if contact.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name is required"})
		return
	}

	// Установка значений по умолчанию
	contact.ID = uuid.New()
	contact.UserID = c.GetString("user_id")
	contact.CompanyID = uuid.MustParse(c.GetString("company_id"))
	contact.Type = "client"
	contact.Status = "active"
	contact.CreatedAt = time.Now()
	contact.UpdatedAt = time.Now()

	// Здесь должна быть логика сохранения в базу данных
	// Пока возвращаем созданный контакт
	c.JSON(http.StatusCreated, contact)
}

// GetContacts godoc
// @Summary Get contacts list
// @Description Get list of contacts with optional filtering
// @Tags contacts
// @Accept json
// @Produce json
// @Param search query string false "Search query"
// @Param type query string false "Type filter"
// @Param status query string false "Status filter"
// @Success 200 {array} models.Contact
// @Failure 400 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/contacts [get]
func (h *ContactHandler) GetContacts(c *gin.Context) {
	userID := c.GetString("user_id")
	companyID := c.GetString("company_id")

	// Получение параметров фильтрации (пока не используются, но будут для фильтрации)
	_ = c.Query("search")
	_ = c.Query("type")
	_ = c.Query("status")

	// Здесь должна быть логика получения контактов из базы данных
	// Пока возвращаем моковые данные
	contacts := []models.Contact{
		{
			ID:        uuid.New(),
			UserID:    userID,
			CompanyID: uuid.MustParse(companyID),
			Name:      "Иван Иванов",
			Email:     "ivan@example.com",
			Phone:     "+7 (999) 123-45-67",
			Company:   "ООО Рога и Копыта",
			Position:  "Директор",
			Type:      "client",
			Status:    "active",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		{
			ID:        uuid.New(),
			UserID:    userID,
			CompanyID: uuid.MustParse(companyID),
			Name:      "Петр Петров",
			Email:     "petr@example.com",
			Phone:     "+7 (999) 765-43-21",
			Company:   "ИП Петров",
			Position:  "Бухгалтер",
			Type:      "contractor",
			Status:    "active",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}

	c.JSON(http.StatusOK, contacts)
}

// GetContact godoc
// @Summary Get contact by ID
// @Description Get contact details by ID
// @Tags contacts
// @Accept json
// @Produce json
// @Param id path string true "Contact ID"
// @Success 200 {object} models.Contact
// @Failure 400 {object} map[string]string "error response"
// @Failure 404 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/contacts/{id} [get]
func (h *ContactHandler) GetContact(c *gin.Context) {
	contactID := c.Param("id")
	userID := c.GetString("user_id")

	// Здесь должна быть логика получения контакта из базы данных
	// Пока возвращаем моковые данные
	contact := models.Contact{
		ID:        uuid.MustParse(contactID),
		UserID:    userID,
		CompanyID: uuid.MustParse(c.GetString("company_id")),
		Name:      "Иван Иванов",
		Email:     "ivan@example.com",
		Phone:     "+7 (999) 123-45-67",
		Company:   "ООО Рога и Копыта",
		Position:  "Директор",
		Type:      "client",
		Status:    "active",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	c.JSON(http.StatusOK, contact)
}

// UpdateContact godoc
// @Summary Update contact
// @Description Update contact details
// @Tags contacts
// @Accept json
// @Produce json
// @Param id path string true "Contact ID"
// @Param contact body models.Contact true "Updated contact data"
// @Success 200 {object} models.Contact
// @Failure 400 {object} map[string]string "error response"
// @Failure 403 {object} map[string]string "error response"
// @Failure 404 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/contacts/{id} [put]
func (h *ContactHandler) UpdateContact(c *gin.Context) {
	contactID := c.Param("id")
	userID := c.GetString("user_id")

	// Валидация существования контакта
	if contactID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Contact ID is required"})
		return
	}

	var contact models.Contact
	if err := c.ShouldBindJSON(&contact); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input: " + err.Error()})
		return
	}

	// Валидация обязательных полей
	if contact.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name is required"})
		return
	}

	// Обновление полей
	contact.ID = uuid.MustParse(contactID)
	contact.UserID = userID
	contact.CompanyID = uuid.MustParse(c.GetString("company_id"))
	contact.UpdatedAt = time.Now()

	// Здесь должна быть логика обновления в базе данных
	// Пока возвращаем обновленный контакт
	c.JSON(http.StatusOK, contact)
}

// DeleteContact godoc
// @Summary Delete contact
// @Description Delete contact by ID
// @Tags contacts
// @Accept json
// @Produce json
// @Param id path string true "Contact ID"
// @Success 200 {object} map[string]string "success response"
// @Failure 400 {object} map[string]string "error response"
// @Failure 403 {object} map[string]string "error response"
// @Failure 404 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/contacts/{id} [delete]
func (h *ContactHandler) DeleteContact(c *gin.Context) {
	contactID := c.Param("id")

	// Валидация существования контакта
	if contactID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Contact ID is required"})
		return
	}

	// Здесь должна быть логика удаления из базы данных
	// Пока возвращаем успешный ответ
	c.JSON(http.StatusOK, gin.H{"message": "Contact deleted successfully"})
}
