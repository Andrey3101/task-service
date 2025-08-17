package services

import (
	"context"
	"errors"
	"time"

	"github.com/andre/task-service/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ContactService struct {
	db *gorm.DB
}

func NewContactService(db *gorm.DB) *ContactService {
	return &ContactService{db: db}
}

// CreateContact создает новый контакт
func (s *ContactService) CreateContact(ctx context.Context, userID string, companyID uuid.UUID, contact *models.Contact) (*models.Contact, error) {
	// Валидация бизнес-логики
	if err := s.validateContact(contact); err != nil {
		return nil, err
	}

	// Проверка уникальности email если указан
	if contact.Email != "" {
		if err := s.checkEmailUniqueness(ctx, userID, companyID, contact.Email); err != nil {
			return nil, err
		}
	}

	// Установка значений по умолчанию
	contact.ID = uuid.New()
	contact.UserID = userID
	contact.CompanyID = companyID
	contact.Status = "active"
	contact.CreatedAt = time.Now()
	contact.UpdatedAt = time.Now()

	// Сохранение в базу данных
	if err := s.db.Create(contact).Error; err != nil {
		return nil, err
	}

	// Отправка уведомления о создании контакта
	go s.sendContactCreationNotification(contact)

	return contact, nil
}

// GetContacts получает список контактов с фильтрацией
func (s *ContactService) GetContacts(ctx context.Context, userID string, companyID uuid.UUID, filters map[string]interface{}) ([]models.Contact, error) {
	var contacts []models.Contact
	query := s.db.Where("company_id = ?", companyID)

	// Применение фильтров
	if contactType, ok := filters["type"].(string); ok {
		query = query.Where("type = ?", contactType)
	}
	if company, ok := filters["company"].(string); ok {
		query = query.Where("company ILIKE ?", "%"+company+"%")
	}
	if status, ok := filters["status"].(string); ok {
		query = query.Where("status = ?", status)
	}
	if search, ok := filters["search"].(string); ok {
		query = query.Where("name ILIKE ? OR email ILIKE ? OR phone ILIKE ?",
			"%"+search+"%", "%"+search+"%", "%"+search+"%")
	}

	// Получение контактов пользователя или всех контактов компании
	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}

	if err := query.Order("name ASC").Find(&contacts).Error; err != nil {
		return nil, err
	}

	return contacts, nil
}

// GetContact получает контакт по ID
func (s *ContactService) GetContact(ctx context.Context, contactID uuid.UUID, userID string, companyID uuid.UUID) (*models.Contact, error) {
	var contact models.Contact
	if err := s.db.Where("id = ? AND company_id = ?", contactID, companyID).First(&contact).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("contact not found")
		}
		return nil, err
	}

	// Проверка доступа пользователя к контакту
	if contact.UserID != userID {
		return nil, errors.New("access denied")
	}

	return &contact, nil
}

// UpdateContact обновляет контакт
func (s *ContactService) UpdateContact(ctx context.Context, contactID uuid.UUID, userID string, companyID uuid.UUID, updates *models.Contact) (*models.Contact, error) {
	// Получение существующего контакта
	existingContact, err := s.GetContact(ctx, contactID, userID, companyID)
	if err != nil {
		return nil, err
	}

	// Валидация обновлений
	if err := s.validateContact(updates); err != nil {
		return nil, err
	}

	// Проверка уникальности email если изменился
	if updates.Email != "" && updates.Email != existingContact.Email {
		if err := s.checkEmailUniqueness(ctx, userID, companyID, updates.Email); err != nil {
			return nil, err
		}
	}

	// Обновление полей
	updates.ID = contactID
	updates.UserID = userID
	updates.CompanyID = companyID
	updates.UpdatedAt = time.Now()

	// Сохранение изменений
	if err := s.db.Model(existingContact).Updates(updates).Error; err != nil {
		return nil, err
	}

	// Отправка уведомления об изменениях
	go s.sendContactUpdateNotification(existingContact, updates)

	return updates, nil
}

// DeleteContact удаляет контакт
func (s *ContactService) DeleteContact(ctx context.Context, contactID uuid.UUID, userID string, companyID uuid.UUID) error {
	// Получение существующего контакта
	existingContact, err := s.GetContact(ctx, contactID, userID, companyID)
	if err != nil {
		return err
	}

	// Удаление взаимодействий с контактом
	if err := s.db.Where("contact_id = ?", contactID).Delete(&models.ContactInteraction{}).Error; err != nil {
		return err
	}

	// Удаление контакта
	if err := s.db.Delete(existingContact).Error; err != nil {
		return err
	}

	// Отправка уведомления об удалении
	go s.sendContactDeletionNotification(existingContact)

	return nil
}

// CreateContactInteraction создает взаимодействие с контактом
func (s *ContactService) CreateContactInteraction(ctx context.Context, contactID uuid.UUID, userID string, interaction *models.ContactInteraction) (*models.ContactInteraction, error) {
	// Проверка существования контакта
	var contact models.Contact
	if err := s.db.Where("id = ?", contactID).First(&contact).Error; err != nil {
		return nil, errors.New("contact not found")
	}

	// Валидация взаимодействия
	if err := s.validateContactInteraction(interaction); err != nil {
		return nil, err
	}

	// Установка значений по умолчанию
	interaction.ID = uuid.New()
	interaction.ContactID = contactID
	interaction.UserID = userID
	interaction.CreatedAt = time.Now()

	// Сохранение в базу данных
	if err := s.db.Create(interaction).Error; err != nil {
		return nil, err
	}

	// Отправка уведомления о взаимодействии
	go s.sendInteractionNotification(&contact, interaction)

	return interaction, nil
}

// GetContactInteractions получает взаимодействия с контактом
func (s *ContactService) GetContactInteractions(ctx context.Context, contactID uuid.UUID, userID string, companyID uuid.UUID, limit int) ([]models.ContactInteraction, error) {
	var interactions []models.ContactInteraction

	// Проверка доступа к контакту
	if _, err := s.GetContact(ctx, contactID, userID, companyID); err != nil {
		return nil, err
	}

	// Получение взаимодействий
	query := s.db.Where("contact_id = ?", contactID)
	if limit > 0 {
		query = query.Limit(limit)
	}

	if err := query.Order("created_at DESC").Find(&interactions).Error; err != nil {
		return nil, err
	}

	return interactions, nil
}

// GetContactsByType получает контакты по типу
func (s *ContactService) GetContactsByType(ctx context.Context, userID string, companyID uuid.UUID, contactType string) ([]models.Contact, error) {
	var contacts []models.Contact
	query := s.db.Where("company_id = ? AND type = ?", companyID, contactType)

	// Получение контактов пользователя или всех контактов компании
	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}

	if err := query.Order("name ASC").Find(&contacts).Error; err != nil {
		return nil, err
	}

	return contacts, nil
}

// GetContactsByCompany получает контакты по компании
func (s *ContactService) GetContactsByCompany(ctx context.Context, userID string, companyID uuid.UUID, companyName string) ([]models.Contact, error) {
	var contacts []models.Contact
	query := s.db.Where("company_id = ? AND company ILIKE ?", companyID, "%"+companyName+"%")

	// Получение контактов пользователя или всех контактов компании
	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}

	if err := query.Order("name ASC").Find(&contacts).Error; err != nil {
		return nil, err
	}

	return contacts, nil
}

// SearchContacts выполняет поиск контактов
func (s *ContactService) SearchContacts(ctx context.Context, userID string, companyID uuid.UUID, query string) ([]models.Contact, error) {
	var contacts []models.Contact
	dbQuery := s.db.Where("company_id = ? AND (name ILIKE ? OR email ILIKE ? OR phone ILIKE ? OR company ILIKE ?)",
		companyID, "%"+query+"%", "%"+query+"%", "%"+query+"%", "%"+query+"%")

	// Получение контактов пользователя или всех контактов компании
	if userID != "" {
		dbQuery = dbQuery.Where("user_id = ?", userID)
	}

	if err := dbQuery.Order("name ASC").Find(&contacts).Error; err != nil {
		return nil, err
	}

	return contacts, nil
}

// GetContactStats получает статистику по контактам
func (s *ContactService) GetContactStats(ctx context.Context, userID string, companyID uuid.UUID) (map[string]interface{}, error) {
	var stats map[string]interface{}

	// Базовый запрос
	baseQuery := s.db.Where("company_id = ?", companyID)
	if userID != "" {
		baseQuery = baseQuery.Where("user_id = ?", userID)
	}

	// Общее количество контактов
	var totalCount int64
	if err := baseQuery.Model(&models.Contact{}).Count(&totalCount).Error; err != nil {
		return nil, err
	}

	// Количество по типам
	var typeStats []struct {
		Type  string `json:"type"`
		Count int64  `json:"count"`
	}
	if err := baseQuery.Model(&models.Contact{}).
		Select("type, count(*) as count").
		Group("type").
		Find(&typeStats).Error; err != nil {
		return nil, err
	}

	// Количество по статусам
	var statusStats []struct {
		Status string `json:"status"`
		Count  int64  `json:"count"`
	}
	if err := baseQuery.Model(&models.Contact{}).
		Select("status, count(*) as count").
		Group("status").
		Find(&statusStats).Error; err != nil {
		return nil, err
	}

	// Количество активных контактов
	var activeCount int64
	if err := baseQuery.Model(&models.Contact{}).Where("status = ?", "active").Count(&activeCount).Error; err != nil {
		return nil, err
	}

	stats = map[string]interface{}{
		"total_contacts":      totalCount,
		"active_contacts":     activeCount,
		"type_distribution":   typeStats,
		"status_distribution": statusStats,
	}

	return stats, nil
}

// Вспомогательные методы

func (s *ContactService) validateContact(contact *models.Contact) error {
	if contact.Name == "" {
		return errors.New("name is required")
	}
	if contact.Type == "" {
		return errors.New("type is required")
	}

	// Валидация типа контакта
	validTypes := []string{"client", "supplier", "partner", "employee", "prospect"}
	valid := false
	for _, validType := range validTypes {
		if contact.Type == validType {
			valid = true
			break
		}
	}
	if !valid {
		return errors.New("invalid contact type")
	}

	// Валидация статуса
	if contact.Status != "" {
		validStatuses := []string{"active", "inactive", "archived"}
		valid = false
		for _, validStatus := range validStatuses {
			if contact.Status == validStatus {
				valid = true
				break
			}
		}
		if !valid {
			return errors.New("invalid status")
		}
	}

	// Валидация email если указан
	if contact.Email != "" {
		// Простая проверка формата email
		if !s.isValidEmail(contact.Email) {
			return errors.New("invalid email format")
		}
	}

	return nil
}

func (s *ContactService) validateContactInteraction(interaction *models.ContactInteraction) error {
	if interaction.Type == "" {
		return errors.New("interaction type is required")
	}
	if interaction.Status == "" {
		return errors.New("interaction status is required")
	}

	// Валидация типа взаимодействия
	validTypes := []string{"call", "email", "meeting", "message"}
	valid := false
	for _, validType := range validTypes {
		if interaction.Type == validType {
			valid = true
			break
		}
	}
	if !valid {
		return errors.New("invalid interaction type")
	}

	// Валидация статуса взаимодействия
	validStatuses := []string{"scheduled", "completed", "cancelled", "failed"}
	valid = false
	for _, validStatus := range validStatuses {
		if interaction.Status == validStatus {
			valid = true
			break
		}
	}
	if !valid {
		return errors.New("invalid interaction status")
	}

	return nil
}

func (s *ContactService) checkEmailUniqueness(ctx context.Context, userID string, companyID uuid.UUID, email string) error {
	var count int64
	err := s.db.Model(&models.Contact{}).
		Where("user_id = ? AND company_id = ? AND email = ?", userID, companyID, email).
		Count(&count).Error

	if err != nil {
		return err
	}

	if count > 0 {
		return errors.New("contact with this email already exists")
	}

	return nil
}

func (s *ContactService) isValidEmail(email string) bool {
	// Простая проверка формата email
	// В реальности здесь будет более сложная валидация
	return len(email) > 3 && len(email) < 255
}

// Методы для уведомлений (заглушки для демонстрации)
func (s *ContactService) sendContactCreationNotification(contact *models.Contact) {
	// Логика отправки уведомления о создании контакта
}

func (s *ContactService) sendContactUpdateNotification(oldContact, newContact *models.Contact) {
	// Логика отправки уведомления об изменении контакта
}

func (s *ContactService) sendContactDeletionNotification(contact *models.Contact) {
	// Логика отправки уведомления об удалении контакта
}

func (s *ContactService) sendInteractionNotification(contact *models.Contact, interaction *models.ContactInteraction) {
	// Логика отправки уведомления о взаимодействии
}
