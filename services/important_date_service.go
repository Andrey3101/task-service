package services

import (
	"context"
	"errors"
	"time"

	"github.com/andre/task-service/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ImportantDateService struct {
	db *gorm.DB
}

func NewImportantDateService(db *gorm.DB) *ImportantDateService {
	return &ImportantDateService{db: db}
}

// CreateImportantDate создает новую важную дату
func (s *ImportantDateService) CreateImportantDate(ctx context.Context, userID string, companyID uuid.UUID, date *models.ImportantDate) (*models.ImportantDate, error) {
	// Валидация бизнес-логики
	if err := s.validateImportantDate(date); err != nil {
		return nil, err
	}

	// Установка значений по умолчанию
	date.ID = uuid.New()
	date.UserID = userID
	date.CompanyID = companyID
	date.CreatedAt = time.Now()
	date.UpdatedAt = time.Now()

	// Сохранение в базу данных
	if err := s.db.Create(date).Error; err != nil {
		return nil, err
	}

	// Создание напоминаний
	if len(date.ReminderDays) > 0 {
		if err := s.createDateReminders(date.ID, date.Date, date.ReminderDays); err != nil {
			return nil, err
		}
	}

	// Отправка уведомления о создании
	go s.sendDateCreationNotification(date)

	return date, nil
}

// GetImportantDates получает список важных дат с фильтрацией
func (s *ImportantDateService) GetImportantDates(ctx context.Context, userID string, companyID uuid.UUID, filters map[string]interface{}) ([]models.ImportantDate, error) {
	var dates []models.ImportantDate
	query := s.db.Where("company_id = ?", companyID)

	// Применение фильтров
	if dateType, ok := filters["type"].(string); ok {
		query = query.Where("type = ?", dateType)
	}
	if priority, ok := filters["priority"].(string); ok {
		query = query.Where("priority = ?", priority)
	}
	if startDate, ok := filters["start_date"].(time.Time); ok {
		query = query.Where("date >= ?", startDate)
	}
	if endDate, ok := filters["end_date"].(time.Time); ok {
		query = query.Where("date <= ?", endDate)
	}

	// Получение дат пользователя или всех дат компании
	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}

	if err := query.Order("date ASC").Find(&dates).Error; err != nil {
		return nil, err
	}

	return dates, nil
}

// GetImportantDate получает важную дату по ID
func (s *ImportantDateService) GetImportantDate(ctx context.Context, dateID uuid.UUID, userID string, companyID uuid.UUID) (*models.ImportantDate, error) {
	var date models.ImportantDate
	if err := s.db.Where("id = ? AND company_id = ?", dateID, companyID).First(&date).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("important date not found")
		}
		return nil, err
	}

	// Проверка доступа пользователя к дате
	if date.UserID != userID {
		return nil, errors.New("access denied")
	}

	return &date, nil
}

// UpdateImportantDate обновляет важную дату
func (s *ImportantDateService) UpdateImportantDate(ctx context.Context, dateID uuid.UUID, userID string, companyID uuid.UUID, updates *models.ImportantDate) (*models.ImportantDate, error) {
	// Получение существующей даты
	existingDate, err := s.GetImportantDate(ctx, dateID, userID, companyID)
	if err != nil {
		return nil, err
	}

	// Валидация обновлений
	if err := s.validateImportantDate(updates); err != nil {
		return nil, err
	}

	// Обновление полей
	updates.ID = dateID
	updates.UserID = userID
	updates.CompanyID = companyID
	updates.UpdatedAt = time.Now()

	// Сохранение изменений
	if err := s.db.Model(existingDate).Updates(updates).Error; err != nil {
		return nil, err
	}

	// Обновление напоминаний если изменились
	if len(updates.ReminderDays) > 0 {
		if err := s.updateDateReminders(dateID, updates.Date, updates.ReminderDays); err != nil {
			return nil, err
		}
	}

	// Отправка уведомления об изменениях
	go s.sendDateUpdateNotification(existingDate, updates)

	return updates, nil
}

// DeleteImportantDate удаляет важную дату
func (s *ImportantDateService) DeleteImportantDate(ctx context.Context, dateID uuid.UUID, userID string, companyID uuid.UUID) error {
	// Получение существующей даты
	existingDate, err := s.GetImportantDate(ctx, dateID, userID, companyID)
	if err != nil {
		return err
	}

	// Удаление напоминаний
	if err := s.db.Where("important_date_id = ?", dateID).Delete(&models.DateReminder{}).Error; err != nil {
		return err
	}

	// Удаление даты
	if err := s.db.Delete(existingDate).Error; err != nil {
		return err
	}

	// Отправка уведомления об удалении
	go s.sendDateDeletionNotification(existingDate)

	return nil
}

// GetUpcomingDates получает предстоящие важные даты
func (s *ImportantDateService) GetUpcomingDates(ctx context.Context, userID string, companyID uuid.UUID, days int) ([]models.ImportantDate, error) {
	var dates []models.ImportantDate
	endDate := time.Now().AddDate(0, 0, days)

	query := s.db.Where("company_id = ? AND date >= ? AND date <= ?", companyID, time.Now(), endDate)

	// Получение дат пользователя или всех дат компании
	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}

	if err := query.Order("date ASC").Find(&dates).Error; err != nil {
		return nil, err
	}

	return dates, nil
}

// GetDatesByType получает даты по типу
func (s *ImportantDateService) GetDatesByType(ctx context.Context, userID string, companyID uuid.UUID, dateType string) ([]models.ImportantDate, error) {
	var dates []models.ImportantDate
	query := s.db.Where("company_id = ? AND type = ?", companyID, dateType)

	// Получение дат пользователя или всех дат компании
	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}

	if err := query.Order("date ASC").Find(&dates).Error; err != nil {
		return nil, err
	}

	return dates, nil
}

// GetOverdueDates получает просроченные даты
func (s *ImportantDateService) GetOverdueDates(ctx context.Context, userID string, companyID uuid.UUID) ([]models.ImportantDate, error) {
	var dates []models.ImportantDate
	query := s.db.Where("company_id = ? AND date < ? AND status != 'completed'", companyID, time.Now())

	// Получение дат пользователя или всех дат компании
	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}

	if err := query.Order("date ASC").Find(&dates).Error; err != nil {
		return nil, err
	}

	return dates, nil
}

// MarkDateCompleted отмечает дату как выполненную
func (s *ImportantDateService) MarkDateCompleted(ctx context.Context, dateID uuid.UUID, userID string, companyID uuid.UUID) error {
	// Получение существующей даты
	existingDate, err := s.GetImportantDate(ctx, dateID, userID, companyID)
	if err != nil {
		return err
	}

	// Обновление статуса
	if err := s.db.Model(existingDate).Update("status", "completed").Error; err != nil {
		return err
	}

	// Отправка уведомления о выполнении
	go s.sendDateCompletionNotification(existingDate)

	return nil
}

// ProcessReminders обрабатывает напоминания
func (s *ImportantDateService) ProcessReminders(ctx context.Context) error {
	var reminders []models.DateReminder
	today := time.Now().Truncate(24 * time.Hour)

	// Получение напоминаний на сегодня
	if err := s.db.Where("reminder_date = ? AND sent = ?", today, false).Find(&reminders).Error; err != nil {
		return err
	}

	for _, reminder := range reminders {
		// Получение важной даты
		var importantDate models.ImportantDate
		if err := s.db.Where("id = ?", reminder.ImportantDateID).First(&importantDate).Error; err != nil {
			continue
		}

		// Отправка напоминания
		if err := s.sendReminder(&importantDate, &reminder); err != nil {
			continue
		}

		// Отметка напоминания как отправленного
		reminder.Sent = true
		reminder.SentAt = time.Now()
		s.db.Save(&reminder)
	}

	return nil
}

// Вспомогательные методы

func (s *ImportantDateService) validateImportantDate(date *models.ImportantDate) error {
	if date.Title == "" {
		return errors.New("title is required")
	}
	if date.Date.IsZero() {
		return errors.New("date is required")
	}
	if date.Type == "" {
		return errors.New("type is required")
	}
	if date.Priority == "" {
		return errors.New("priority is required")
	}

	// Валидация типа даты
	validTypes := []string{"tax_deadline", "contract_expiry", "payment_due", "birthday", "anniversary", "other"}
	valid := false
	for _, validType := range validTypes {
		if date.Type == validType {
			valid = true
			break
		}
	}
	if !valid {
		return errors.New("invalid date type")
	}

	// Валидация приоритета
	validPriorities := []string{"low", "medium", "high", "critical"}
	valid = false
	for _, validPriority := range validPriorities {
		if date.Priority == validPriority {
			valid = true
			break
		}
	}
	if !valid {
		return errors.New("invalid priority")
	}

	return nil
}

func (s *ImportantDateService) createDateReminders(dateID uuid.UUID, date time.Time, reminderDays []int) error {
	for _, days := range reminderDays {
		if days < 0 {
			continue // Пропускаем отрицательные значения
		}

		reminderDate := date.AddDate(0, 0, -days)
		if reminderDate.Before(time.Now()) {
			continue // Пропускаем напоминания в прошлом
		}

		reminder := models.DateReminder{
			ID:              uuid.New(),
			ImportantDateID: dateID,
			ReminderDate:    reminderDate,
			Sent:            false,
			Channel:         "email", // По умолчанию email
			CreatedAt:       time.Now(),
		}

		if err := s.db.Create(&reminder).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *ImportantDateService) updateDateReminders(dateID uuid.UUID, date time.Time, reminderDays []int) error {
	// Удаление существующих напоминаний
	if err := s.db.Where("important_date_id = ?", dateID).Delete(&models.DateReminder{}).Error; err != nil {
		return err
	}

	// Создание новых напоминаний
	return s.createDateReminders(dateID, date, reminderDays)
}

// Методы для уведомлений (заглушки для демонстрации)
func (s *ImportantDateService) sendDateCreationNotification(date *models.ImportantDate) {
	// Логика отправки уведомления о создании важной даты
}

func (s *ImportantDateService) sendDateUpdateNotification(oldDate, newDate *models.ImportantDate) {
	// Логика отправки уведомления об изменении важной даты
}

func (s *ImportantDateService) sendDateDeletionNotification(date *models.ImportantDate) {
	// Логика отправки уведомления об удалении важной даты
}

func (s *ImportantDateService) sendDateCompletionNotification(date *models.ImportantDate) {
	// Логика отправки уведомления о выполнении важной даты
}

func (s *ImportantDateService) sendReminder(importantDate *models.ImportantDate, reminder *models.DateReminder) error {
	// Логика отправки напоминания
	// Здесь будет интеграция с email, SMS, push-уведомлениями
	return nil
}
