package services

import (
	"context"
	"errors"
	"time"

	"github.com/andre/task-service/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type MeetingService struct {
	db *gorm.DB
}

func NewMeetingService(db *gorm.DB) *MeetingService {
	return &MeetingService{db: db}
}

// CreateMeeting создает новую встречу
func (s *MeetingService) CreateMeeting(ctx context.Context, userID string, companyID uuid.UUID, meeting *models.Meeting) (*models.Meeting, error) {
	// Валидация бизнес-логики
	if err := s.validateMeeting(meeting); err != nil {
		return nil, err
	}

	// Проверка конфликтов по времени
	if err := s.checkTimeConflicts(ctx, userID, companyID, meeting); err != nil {
		return nil, err
	}

	// Установка значений по умолчанию
	meeting.ID = uuid.New()
	meeting.UserID = userID
	meeting.CompanyID = companyID
	meeting.Status = "scheduled"
	meeting.CreatedAt = time.Now()
	meeting.UpdatedAt = time.Now()

	// Сохранение в базу данных
	if err := s.db.Create(meeting).Error; err != nil {
		return nil, err
	}

	// Создание участников встречи
	if len(meeting.Participants) > 0 {
		if err := s.createMeetingParticipants(meeting.ID, meeting.Participants); err != nil {
			return nil, err
		}
	}

	// Отправка уведомлений участникам
	go s.sendMeetingNotifications(meeting)

	return meeting, nil
}

// GetMeetings получает список встреч с фильтрацией
func (s *MeetingService) GetMeetings(ctx context.Context, userID string, companyID uuid.UUID, filters map[string]interface{}) ([]models.Meeting, error) {
	var meetings []models.Meeting
	query := s.db.Where("company_id = ?", companyID)

	// Применение фильтров
	if startDate, ok := filters["start_date"].(time.Time); ok {
		query = query.Where("start_time >= ?", startDate)
	}
	if endDate, ok := filters["end_date"].(time.Time); ok {
		query = query.Where("end_time <= ?", endDate)
	}
	if meetingType, ok := filters["meeting_type"].(string); ok {
		query = query.Where("meeting_type = ?", meetingType)
	}
	if status, ok := filters["status"].(string); ok {
		query = query.Where("status = ?", status)
	}

	// Получение встреч пользователя или всех встреч компании
	if userID != "" {
		query = query.Where("user_id = ? OR participants @> ?", userID, []string{userID})
	}

	if err := query.Order("start_time ASC").Find(&meetings).Error; err != nil {
		return nil, err
	}

	return meetings, nil
}

// GetMeeting получает встречу по ID
func (s *MeetingService) GetMeeting(ctx context.Context, meetingID uuid.UUID, userID string, companyID uuid.UUID) (*models.Meeting, error) {
	var meeting models.Meeting
	if err := s.db.Where("id = ? AND company_id = ?", meetingID, companyID).First(&meeting).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("meeting not found")
		}
		return nil, err
	}

	// Проверка доступа пользователя к встрече
	if meeting.UserID != userID && !s.isParticipant(meetingID, userID) {
		return nil, errors.New("access denied")
	}

	return &meeting, nil
}

// UpdateMeeting обновляет встречу
func (s *MeetingService) UpdateMeeting(ctx context.Context, meetingID uuid.UUID, userID string, companyID uuid.UUID, updates *models.Meeting) (*models.Meeting, error) {
	// Получение существующей встречи
	existingMeeting, err := s.GetMeeting(ctx, meetingID, userID, companyID)
	if err != nil {
		return nil, err
	}

	// Проверка прав на редактирование
	if existingMeeting.UserID != userID {
		return nil, errors.New("only meeting organizer can edit meeting")
	}

	// Валидация обновлений
	if err := s.validateMeeting(updates); err != nil {
		return nil, err
	}

	// Проверка конфликтов по времени (исключая текущую встречу)
	if err := s.checkTimeConflictsExcluding(ctx, userID, companyID, updates, meetingID); err != nil {
		return nil, err
	}

	// Обновление полей
	updates.ID = meetingID
	updates.UserID = userID
	updates.CompanyID = companyID
	updates.UpdatedAt = time.Now()

	// Сохранение изменений
	if err := s.db.Model(existingMeeting).Updates(updates).Error; err != nil {
		return nil, err
	}

	// Обновление участников если изменились
	if len(updates.Participants) > 0 {
		if err := s.updateMeetingParticipants(meetingID, updates.Participants); err != nil {
			return nil, err
		}
	}

	// Отправка уведомлений об изменениях
	go s.sendMeetingUpdateNotifications(existingMeeting, updates)

	return updates, nil
}

// DeleteMeeting удаляет встречу
func (s *MeetingService) DeleteMeeting(ctx context.Context, meetingID uuid.UUID, userID string, companyID uuid.UUID) error {
	// Получение существующей встречи
	existingMeeting, err := s.GetMeeting(ctx, meetingID, userID, companyID)
	if err != nil {
		return err
	}

	// Проверка прав на удаление
	if existingMeeting.UserID != userID {
		return errors.New("only meeting organizer can delete meeting")
	}

	// Удаление участников встречи
	if err := s.db.Where("meeting_id = ?", meetingID).Delete(&models.MeetingParticipant{}).Error; err != nil {
		return err
	}

	// Удаление встречи
	if err := s.db.Delete(existingMeeting).Error; err != nil {
		return err
	}

	// Отправка уведомлений об отмене
	go s.sendMeetingCancellationNotifications(existingMeeting)

	return nil
}

// GetUpcomingMeetings получает предстоящие встречи
func (s *MeetingService) GetUpcomingMeetings(ctx context.Context, userID string, companyID uuid.UUID, days int) ([]models.Meeting, error) {
	var meetings []models.Meeting
	endDate := time.Now().AddDate(0, 0, days)

	query := s.db.Where("company_id = ? AND start_time >= ? AND start_time <= ? AND status = ?",
		companyID, time.Now(), endDate, "scheduled")

	// Получение встреч пользователя или всех встреч компании
	if userID != "" {
		query = query.Where("user_id = ? OR participants @> ?", userID, []string{userID})
	}

	if err := query.Order("start_time ASC").Find(&meetings).Error; err != nil {
		return nil, err
	}

	return meetings, nil
}

// ChangeMeetingStatus изменяет статус встречи
func (s *MeetingService) ChangeMeetingStatus(ctx context.Context, meetingID uuid.UUID, userID string, companyID uuid.UUID, status string) error {
	// Получение существующей встречи
	existingMeeting, err := s.GetMeeting(ctx, meetingID, userID, companyID)
	if err != nil {
		return err
	}

	// Проверка прав на изменение статуса
	if existingMeeting.UserID != userID {
		return errors.New("only meeting organizer can change meeting status")
	}

	// Валидация статуса
	validStatuses := []string{"scheduled", "confirmed", "cancelled", "completed"}
	valid := false
	for _, validStatus := range validStatuses {
		if status == validStatus {
			valid = true
			break
		}
	}
	if !valid {
		return errors.New("invalid meeting status")
	}

	// Обновление статуса
	if err := s.db.Model(existingMeeting).Update("status", status).Error; err != nil {
		return err
	}

	// Отправка уведомлений об изменении статуса
	go s.sendMeetingStatusChangeNotifications(existingMeeting, status)

	return nil
}

// Вспомогательные методы

func (s *MeetingService) validateMeeting(meeting *models.Meeting) error {
	if meeting.Title == "" {
		return errors.New("meeting title is required")
	}
	if meeting.StartTime.IsZero() || meeting.EndTime.IsZero() {
		return errors.New("start time and end time are required")
	}
	if meeting.StartTime.After(meeting.EndTime) {
		return errors.New("start time must be before end time")
	}
	if meeting.StartTime.Before(time.Now()) {
		return errors.New("meeting cannot be scheduled in the past")
	}
	return nil
}

func (s *MeetingService) checkTimeConflicts(ctx context.Context, userID string, companyID uuid.UUID, meeting *models.Meeting) error {
	var count int64
	err := s.db.Model(&models.Meeting{}).
		Where("company_id = ? AND status != 'cancelled' AND ((start_time <= ? AND end_time > ?) OR (start_time < ? AND end_time >= ?) OR (start_time >= ? AND end_time <= ?))",
			companyID, meeting.StartTime, meeting.StartTime, meeting.EndTime, meeting.EndTime, meeting.StartTime, meeting.EndTime).
		Count(&count).Error

	if err != nil {
		return err
	}

	if count > 0 {
		return errors.New("time conflict detected with existing meetings")
	}

	return nil
}

func (s *MeetingService) checkTimeConflictsExcluding(ctx context.Context, userID string, companyID uuid.UUID, meeting *models.Meeting, excludeID uuid.UUID) error {
	var count int64
	err := s.db.Model(&models.Meeting{}).
		Where("company_id = ? AND id != ? AND status != 'cancelled' AND ((start_time <= ? AND end_time > ?) OR (start_time < ? AND end_time >= ?) OR (start_time >= ? AND end_time <= ?))",
			companyID, excludeID, meeting.StartTime, meeting.StartTime, meeting.EndTime, meeting.EndTime, meeting.StartTime, meeting.EndTime).
		Count(&count).Error

	if err != nil {
		return err
	}

	if count > 0 {
		return errors.New("time conflict detected with existing meetings")
	}

	return nil
}

func (s *MeetingService) createMeetingParticipants(meetingID uuid.UUID, participants []string) error {
	for _, participantID := range participants {
		participant := models.MeetingParticipant{
			ID:        uuid.New(),
			MeetingID: meetingID,
			UserID:    participantID,
			Role:      "participant",
			Status:    "invited",
			CreatedAt: time.Now(),
		}
		if err := s.db.Create(&participant).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *MeetingService) updateMeetingParticipants(meetingID uuid.UUID, participants []string) error {
	// Удаление существующих участников
	if err := s.db.Where("meeting_id = ?", meetingID).Delete(&models.MeetingParticipant{}).Error; err != nil {
		return err
	}

	// Создание новых участников
	return s.createMeetingParticipants(meetingID, participants)
}

func (s *MeetingService) isParticipant(meetingID uuid.UUID, userID string) bool {
	var count int64
	s.db.Model(&models.MeetingParticipant{}).
		Where("meeting_id = ? AND user_id = ?", meetingID, userID).
		Count(&count)
	return count > 0
}

// Методы для уведомлений (заглушки для демонстрации)
func (s *MeetingService) sendMeetingNotifications(meeting *models.Meeting) {
	// Здесь будет логика отправки уведомлений участникам
	// Например, через email, SMS, push-уведомления
}

func (s *MeetingService) sendMeetingUpdateNotifications(oldMeeting, newMeeting *models.Meeting) {
	// Логика отправки уведомлений об изменениях
}

func (s *MeetingService) sendMeetingCancellationNotifications(meeting *models.Meeting) {
	// Логика отправки уведомлений об отмене
}

func (s *MeetingService) sendMeetingStatusChangeNotifications(meeting *models.Meeting, newStatus string) {
	// Логика отправки уведомлений об изменении статуса
}
