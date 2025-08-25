package services

import (
	"context"
	"time"

	"github.com/andre/task-service/models"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CalendarService сервис для работы с календарем
type CalendarService struct {
	db    *gorm.DB
	redis *redis.Client
}

// NewCalendarService создает новый экземпляр CalendarService
func NewCalendarService(db *gorm.DB, redis *redis.Client) *CalendarService {
	return &CalendarService{
		db:    db,
		redis: redis,
	}
}

// CreateEvent создает новое событие в календаре
func (s *CalendarService) CreateEvent(ctx context.Context, event *models.CalendarEvent) error {
	event.ID = uuid.New()
	event.CreatedAt = time.Now()
	event.UpdatedAt = time.Now()

	if event.Status == "" {
		event.Status = "scheduled"
	}
	if event.Priority == "" {
		event.Priority = "medium"
	}
	if event.Color == "" {
		event.Color = "#3B82F6"
	}

	return s.db.WithContext(ctx).Create(event).Error
}

// GetEventByID получает событие по ID
func (s *CalendarService) GetEventByID(ctx context.Context, eventID uuid.UUID) (*models.CalendarEvent, error) {
	var event models.CalendarEvent
	err := s.db.WithContext(ctx).
		Preload("Reminders").
		Preload("Attachments").
		Preload("Tags").
		Where("id = ?", eventID).
		First(&event).Error
	if err != nil {
		return nil, err
	}
	return &event, nil
}

// GetUserEvents получает события пользователя за период
func (s *CalendarService) GetUserEvents(ctx context.Context, userID string, startDate, endDate time.Time) ([]models.CalendarEvent, error) {
	var events []models.CalendarEvent
	err := s.db.WithContext(ctx).
		Preload("Reminders").
		Preload("Attachments").
		Preload("Tags").
		Where("user_id = ? AND start_time >= ? AND start_time <= ?", userID, startDate, endDate).
		Order("start_time ASC").
		Find(&events).Error
	return events, err
}

// UpdateEvent обновляет событие
func (s *CalendarService) UpdateEvent(ctx context.Context, event *models.CalendarEvent) error {
	event.UpdatedAt = time.Now()
	return s.db.WithContext(ctx).Save(event).Error
}

// DeleteEvent удаляет событие
func (s *CalendarService) DeleteEvent(ctx context.Context, eventID uuid.UUID) error {
	return s.db.WithContext(ctx).Delete(&models.CalendarEvent{}, eventID).Error
}

// SyncTaskToCalendar синхронизирует задачу в календарь
func (s *CalendarService) SyncTaskToCalendar(ctx context.Context, task *models.Task) error {
	// Проверяем настройки пользователя
	var settings models.CalendarSettings
	err := s.db.WithContext(ctx).Where("user_id = ?", task.CreatedBy).First(&settings).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// Создаем настройки по умолчанию
			settings = models.CalendarSettings{
				ID:                uuid.New(),
				UserID:            task.CreatedBy,
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
			s.db.WithContext(ctx).Create(&settings)
		} else {
			return err
		}
	}

	// Если автосинхронизация отключена, не создаем событие
	if !settings.AutoSyncTasks {
		return nil
	}

	// Проверяем, есть ли уже событие для этой задачи
	var existingEvent models.CalendarEvent
	err = s.db.WithContext(ctx).Where("event_type = 'task' AND user_id = ?", task.CreatedBy).First(&existingEvent).Error

	if err == gorm.ErrRecordNotFound {
		// Создаем новое событие
		priorityStr := s.getPriorityString(task.Priority)
		event := &models.CalendarEvent{
			UserID:      task.CreatedBy,
			CompanyID:   task.CompanyID,
			Title:       task.Title,
			Description: task.Description,
			StartTime:   task.DueDate,
			EndTime:     task.DueDate.Add(time.Hour), // По умолчанию 1 час
			EventType:   "task",
			Priority:    priorityStr,
			Status:      "scheduled",
			Color:       s.getPriorityColor(priorityStr),
			IsAllDay:    false,
			Recurring:   "none",
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		// Добавляем напоминание
		reminder := models.CalendarReminder{
			ID:        uuid.New(),
			EventID:   event.ID,
			Type:      "push",
			Minutes:   15, // За 15 минут
			IsSent:    false,
			CreatedAt: time.Now(),
		}
		event.Reminders = append(event.Reminders, reminder)

		return s.CreateEvent(ctx, event)
	} else if err != nil {
		return err
	} else {
		// Обновляем существующее событие
		priorityStr := s.getPriorityString(task.Priority)
		existingEvent.Title = task.Title
		existingEvent.Description = task.Description
		existingEvent.StartTime = task.DueDate
		existingEvent.EndTime = task.DueDate.Add(time.Hour)
		existingEvent.Priority = priorityStr
		existingEvent.Status = s.mapTaskStatus(task.Status)
		existingEvent.UpdatedAt = time.Now()

		return s.UpdateEvent(ctx, &existingEvent)
	}
}

// SyncMeetingToCalendar синхронизирует встречу в календарь
func (s *CalendarService) SyncMeetingToCalendar(ctx context.Context, meeting *models.Meeting) error {
	// Проверяем настройки пользователя
	var settings models.CalendarSettings
	err := s.db.WithContext(ctx).Where("user_id = ?", meeting.UserID).First(&settings).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// Создаем настройки по умолчанию
			settings = models.CalendarSettings{
				ID:                uuid.New(),
				UserID:            meeting.UserID,
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
			s.db.WithContext(ctx).Create(&settings)
		} else {
			return err
		}
	}

	// Если автосинхронизация отключена, не создаем событие
	if !settings.AutoSyncMeetings {
		return nil
	}

	// Проверяем, есть ли уже событие для этой встречи
	var existingEvent models.CalendarEvent
	err = s.db.WithContext(ctx).Where("event_type = 'meeting' AND user_id = ?", meeting.UserID).First(&existingEvent).Error

	if err == gorm.ErrRecordNotFound {
		// Создаем новое событие
		event := &models.CalendarEvent{
			UserID:      meeting.UserID,
			CompanyID:   meeting.CompanyID,
			Title:       meeting.Title,
			Description: meeting.Description,
			StartTime:   meeting.StartTime,
			EndTime:     meeting.EndTime,
			Location:    meeting.Location,
			EventType:   "meeting",
			Priority:    "medium",
			Status:      meeting.Status,
			Color:       "#10B981", // Зеленый для встреч
			IsAllDay:    false,
			Recurring:   "none",
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		// Добавляем напоминание
		reminder := models.CalendarReminder{
			ID:        uuid.New(),
			EventID:   event.ID,
			Type:      "push",
			Minutes:   30, // За 30 минут
			IsSent:    false,
			CreatedAt: time.Now(),
		}
		event.Reminders = append(event.Reminders, reminder)

		return s.CreateEvent(ctx, event)
	} else if err != nil {
		return err
	} else {
		// Обновляем существующее событие
		existingEvent.Title = meeting.Title
		existingEvent.Description = meeting.Description
		existingEvent.StartTime = meeting.StartTime
		existingEvent.EndTime = meeting.EndTime
		existingEvent.Location = meeting.Location
		existingEvent.Status = meeting.Status
		existingEvent.UpdatedAt = time.Now()

		return s.UpdateEvent(ctx, &existingEvent)
	}
}

// GetUserCalendarSettings получает настройки календаря пользователя
func (s *CalendarService) GetUserCalendarSettings(ctx context.Context, userID string) (*models.CalendarSettings, error) {
	var settings models.CalendarSettings
	err := s.db.WithContext(ctx).Where("user_id = ?", userID).First(&settings).Error
	if err != nil {
		return nil, err
	}
	return &settings, nil
}

// UpdateUserCalendarSettings обновляет настройки календаря пользователя
func (s *CalendarService) UpdateUserCalendarSettings(ctx context.Context, settings *models.CalendarSettings) error {
	settings.UpdatedAt = time.Now()
	return s.db.WithContext(ctx).Save(settings).Error
}

// AddReminder добавляет напоминание к событию
func (s *CalendarService) AddReminder(ctx context.Context, reminder *models.CalendarReminder) error {
	reminder.ID = uuid.New()
	reminder.CreatedAt = time.Now()
	return s.db.WithContext(ctx).Create(reminder).Error
}

// AddAttachment добавляет вложение к событию
func (s *CalendarService) AddAttachment(ctx context.Context, attachment *models.CalendarAttachment) error {
	attachment.ID = uuid.New()
	attachment.CreatedAt = time.Now()
	return s.db.WithContext(ctx).Create(attachment).Error
}

// CreateTag создает новый тег
func (s *CalendarService) CreateTag(ctx context.Context, tag *models.CalendarTag) error {
	tag.ID = uuid.New()
	tag.CreatedAt = time.Now()
	return s.db.WithContext(ctx).Create(tag).Error
}

// GetUserTags получает теги пользователя
func (s *CalendarService) GetUserTags(ctx context.Context, userID string) ([]models.CalendarTag, error) {
	var tags []models.CalendarTag
	err := s.db.WithContext(ctx).
		Joins("JOIN calendar_event_tags cet ON calendar_tags.id = cet.calendar_tag_id").
		Joins("JOIN calendar_events ce ON cet.calendar_event_id = ce.id").
		Where("ce.user_id = ?", userID).
		Distinct().
		Find(&tags).Error
	return tags, err
}

// LogSyncAction логирует действие синхронизации
func (s *CalendarService) LogSyncAction(ctx context.Context, log *models.CalendarSyncLog) error {
	log.ID = uuid.New()
	log.CreatedAt = time.Now()
	return s.db.WithContext(ctx).Create(log).Error
}

// getPriorityString возвращает строковое представление приоритета
func (s *CalendarService) getPriorityString(priority int) string {
	switch priority {
	case 1:
		return "low"
	case 2:
		return "medium"
	case 3:
		return "high"
	case 4:
		return "urgent"
	default:
		return "medium"
	}
}

// getPriorityColor возвращает цвет для приоритета
func (s *CalendarService) getPriorityColor(priority string) string {
	switch priority {
	case "low":
		return "#6B7280" // Серый
	case "medium":
		return "#3B82F6" // Синий
	case "high":
		return "#F59E0B" // Желтый
	case "urgent":
		return "#EF4444" // Красный
	default:
		return "#3B82F6" // Синий по умолчанию
	}
}

// mapTaskStatus маппит статус задачи в статус события
func (s *CalendarService) mapTaskStatus(taskStatus string) string {
	switch taskStatus {
	case "pending":
		return "scheduled"
	case "in_progress":
		return "in_progress"
	case "completed":
		return "completed"
	case "cancelled":
		return "cancelled"
	default:
		return "scheduled"
	}
}
