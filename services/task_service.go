package services

import (
	"context"
	"log"
	"time"

	"github.com/andre/task-service/models"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TaskService сервис для работы с задачами
type TaskService struct {
	DB          *gorm.DB
	redis       *redis.Client
	Calendar    *CalendarService // Заменяем Google Calendar на локальный
	rabbitMQURL string
}

// NewTaskService создает новый экземпляр TaskService
func NewTaskService(db *gorm.DB, redisClient *redis.Client, calendarService *CalendarService, rabbitMQURL string) (*TaskService, error) {
	return &TaskService{
		DB:          db,
		redis:       redisClient,
		Calendar:    calendarService,
		rabbitMQURL: rabbitMQURL,
	}, nil
}

// CreateTask создает новую задачу
func (s *TaskService) CreateTask(ctx context.Context, userID string, task *models.Task, isPrediction bool) (*models.Task, error) {
	// Установка значений по умолчанию
	task.ID = uuid.New()
	task.CreatedBy = userID
	task.CreatedAt = time.Now()

	if task.Status == "" {
		task.Status = "pending"
	}
	if task.Priority == 0 {
		task.Priority = 2 // medium по умолчанию
	}

	// Сохранение в базу данных
	if err := s.DB.WithContext(ctx).Create(task).Error; err != nil {
		return nil, err
	}

	// Синхронизация с календарем
	if s.Calendar != nil {
		if err := s.Calendar.SyncTaskToCalendar(ctx, task); err != nil {
			log.Printf("Ошибка синхронизации задачи в календарь: %v", err)
		}
	}

	return task, nil
}

// CreateTaskWithPrediction создает задачу с предсказаниями на основе текста
func (s *TaskService) CreateTaskWithPrediction(ctx context.Context, userID string, text string, confirmSuggestion bool, suggestionAction map[string]interface{}) (*models.Task, error) {
	// Здесь должна быть логика AI для анализа текста и создания задачи
	// Пока что создаем простую задачу
	task := &models.Task{
		Title:       "Задача на основе текста",
		Description: text,
		Status:      "pending",
		Priority:    2,
		Stage:       "lead",
		Channel:     "ai_prediction",
	}

	return s.CreateTask(ctx, userID, task, true)
}

// GetTask получает задачу по ID
func (s *TaskService) GetTask(ctx context.Context, taskID uuid.UUID) (*models.Task, error) {
	var task models.Task
	if err := s.DB.WithContext(ctx).
		Preload("Assignees").
		Preload("Comments").
		Where("id = ?", taskID).
		First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// GetTasks получает список задач с фильтрацией и пагинацией
func (s *TaskService) GetTasks(ctx context.Context, status, dueDate, priority, assigneeID string, fields []string, limit, offset int) ([]models.Task, int64, error) {
	var tasks []models.Task
	var total int64

	query := s.DB.WithContext(ctx).Model(&models.Task{})

	// Применение фильтров
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if dueDate != "" {
		// Здесь должна быть логика парсинга даты
		query = query.Where("due_date = ?", dueDate)
	}
	if priority != "" {
		query = query.Where("priority = ?", priority)
	}
	if assigneeID != "" {
		query = query.Joins("JOIN task_assignees ta ON tasks.id = ta.task_id").
			Where("ta.user_id = ?", assigneeID)
	}

	// Подсчет общего количества
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Применение пагинации и загрузка связанных данных
	if err := query.
		Preload("Assignees").
		Preload("Comments").
		Limit(limit).
		Offset(offset).
		Order("created_at DESC").
		Find(&tasks).Error; err != nil {
		return nil, 0, err
	}

	return tasks, total, nil
}

// GetTaskStats получает статистику по задачам
func (s *TaskService) GetTaskStats(ctx context.Context, userID, status, period string) (map[string]int, error) {
	stats := make(map[string]int)

	query := s.DB.WithContext(ctx).Model(&models.Task{}).Where("created_by = ?", userID)

	// Фильтр по статусу
	if status != "" {
		query = query.Where("status = ?", status)
	}

	// Фильтр по периоду
	if period != "" {
		now := time.Now()
		switch period {
		case "today":
			query = query.Where("created_at >= ?", time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()))
		case "yesterday":
			yesterday := now.AddDate(0, 0, -1)
			query = query.Where("created_at >= ? AND created_at < ?",
				time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, yesterday.Location()),
				time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()))
		case "week":
			weekAgo := now.AddDate(0, 0, -7)
			query = query.Where("created_at >= ?", weekAgo)
		case "month":
			monthAgo := now.AddDate(0, -1, 0)
			query = query.Where("created_at >= ?", monthAgo)
		case "year":
			yearAgo := now.AddDate(-1, 0, 0)
			query = query.Where("created_at >= ?", yearAgo)
		}
	}

	// Подсчет задач по статусам
	var pendingCount, inProgressCount, completedCount int64

	s.DB.WithContext(ctx).Model(&models.Task{}).Where("created_by = ? AND status = ?", userID, "pending").Count(&pendingCount)
	s.DB.WithContext(ctx).Model(&models.Task{}).Where("created_by = ? AND status = ?", userID, "in_progress").Count(&inProgressCount)
	s.DB.WithContext(ctx).Model(&models.Task{}).Where("created_by = ? AND status = ?", userID, "completed").Count(&completedCount)

	stats["pending"] = int(pendingCount)
	stats["in_progress"] = int(inProgressCount)
	stats["completed"] = int(completedCount)
	stats["total"] = int(pendingCount + inProgressCount + completedCount)

	return stats, nil
}

// UpdateTask обновляет задачу
func (s *TaskService) UpdateTask(ctx context.Context, userID string, task *models.Task) (*models.Task, error) {
	// Получаем существующую задачу
	var existingTask models.Task
	if err := s.DB.WithContext(ctx).Where("id = ?", task.ID).First(&existingTask).Error; err != nil {
		return nil, err
	}

	// Обновляем поля
	existingTask.Title = task.Title
	existingTask.Description = task.Description
	existingTask.Status = task.Status
	existingTask.Priority = task.Priority
	existingTask.DueDate = task.DueDate
	existingTask.Cost = task.Cost
	existingTask.Stage = task.Stage
	existingTask.Channel = task.Channel

	// Сохраняем изменения
	if err := s.DB.WithContext(ctx).Save(&existingTask).Error; err != nil {
		return nil, err
	}

	// Обновляем событие в локальном календаре
	if s.Calendar != nil {
		if err := s.Calendar.SyncTaskToCalendar(ctx, &existingTask); err != nil {
			log.Printf("Ошибка синхронизации задачи в календарь: %v", err)
		}
	}

	return &existingTask, nil
}

// DeleteTask удаляет задачу
func (s *TaskService) DeleteTask(ctx context.Context, taskID string) error {
	// Получаем задачу для удаления события из календаря
	var task models.Task
	if err := s.DB.WithContext(ctx).Where("id = ?", taskID).First(&task).Error; err != nil {
		return err
	}

	// Удаляем задачу
	if err := s.DB.WithContext(ctx).Delete(&task).Error; err != nil {
		return err
	}

	// Удаляем событие из локального календаря
	if s.Calendar != nil {
		// Находим и удаляем связанное событие
		events, err := s.Calendar.GetUserEvents(ctx, task.CreatedBy, task.DueDate.AddDate(0, 0, -1), task.DueDate.AddDate(0, 0, 1))
		if err == nil {
			for _, event := range events {
				if event.EventType == "task" && event.Title == task.Title {
					s.Calendar.DeleteEvent(ctx, event.ID)
					break
				}
			}
		}
	}

	return nil
}

// AddComment добавляет комментарий к задаче
func (s *TaskService) AddComment(ctx context.Context, taskID uuid.UUID, userID string, content string) (*models.Comment, error) {
	comment := &models.Comment{
		ID:        uuid.New(),
		TaskID:    taskID,
		UserID:    userID,
		Text:      content,
		CreatedAt: time.Now(),
	}

	if err := s.DB.WithContext(ctx).Create(comment).Error; err != nil {
		return nil, err
	}

	return comment, nil
}

// GetTaskComments получает комментарии к задаче
func (s *TaskService) GetTaskComments(ctx context.Context, taskID uuid.UUID) ([]models.Comment, error) {
	var comments []models.Comment
	if err := s.DB.WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("created_at ASC").
		Find(&comments).Error; err != nil {
		return nil, err
	}
	return comments, nil
}

// GetMyTasks получает задачи пользователя
func (s *TaskService) GetMyTasks(ctx context.Context, userID string, limit, offset int, search string) ([]models.Task, int64, error) {
	var tasks []models.Task
	var total int64

	query := s.DB.WithContext(ctx).Where("created_by = ?", userID)

	if search != "" {
		query = query.Where("title ILIKE ? OR description ILIKE ?", "%"+search+"%", "%"+search+"%")
	}

	// Подсчет общего количества
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Применение пагинации
	if err := query.
		Limit(limit).
		Offset(offset).
		Order("created_at DESC").
		Find(&tasks).Error; err != nil {
		return nil, 0, err
	}

	return tasks, total, nil
}

// GetTaskSuggestions получает предложения по задачам
func (s *TaskService) GetTaskSuggestions(ctx context.Context, userID string, text string) ([]map[string]interface{}, error) {
	// Здесь должна быть логика AI для предложений
	// Пока возвращаем пустой список
	return []map[string]interface{}{}, nil
}

// UpdateComment обновляет комментарий
func (s *TaskService) UpdateComment(ctx context.Context, taskID, commentID uuid.UUID, userID string, text string) (*models.Comment, error) {
	var comment models.Comment
	if err := s.DB.WithContext(ctx).Where("id = ? AND user_id = ?", commentID, userID).First(&comment).Error; err != nil {
		return nil, err
	}

	comment.Text = text
	if err := s.DB.WithContext(ctx).Save(&comment).Error; err != nil {
		return nil, err
	}

	return &comment, nil
}

// BulkAssignTasks массово назначает задачи
func (s *TaskService) BulkAssignTasks(ctx context.Context, userID string, taskIDs []uuid.UUID, assigneeID string) error {
	// Здесь должна быть логика массового назначения
	// Пока что просто логируем
	return nil
}

// DeleteComment удаляет комментарий
func (s *TaskService) DeleteComment(ctx context.Context, taskID, commentID uuid.UUID, userID string) error {
	var comment models.Comment
	if err := s.DB.WithContext(ctx).Where("id = ? AND user_id = ?", commentID, userID).First(&comment).Error; err != nil {
		return err
	}

	return s.DB.WithContext(ctx).Delete(&comment).Error
}

// GenerateReport генерирует отчет
func (s *TaskService) GenerateReport(ctx context.Context, userID string, reportType string) (map[string]interface{}, error) {
	// Здесь должна быть логика генерации отчетов
	// Пока возвращаем пустой отчет
	return map[string]interface{}{
		"type":    reportType,
		"user_id": userID,
		"data":    map[string]interface{}{},
	}, nil
}

// GetReports получает список отчетов
func (s *TaskService) GetReports(ctx context.Context, userID string) ([]map[string]interface{}, error) {
	// Здесь должна быть логика получения отчетов
	// Пока возвращаем пустой список
	return []map[string]interface{}{}, nil
}

// GetDashboard получает данные для дашборда
func (s *TaskService) GetDashboard(ctx context.Context, userID string, filters map[string]string) (map[string]interface{}, error) {
	// Здесь должна быть логика получения данных дашборда
	// Пока возвращаем пустые данные
	return map[string]interface{}{
		"user_id": userID,
		"filters": filters,
		"stats":   map[string]interface{}{},
		"tasks":   []interface{}{},
	}, nil
}

// CreateMeeting создает новую встречу
func (s *TaskService) CreateMeeting(ctx context.Context, meeting *models.Meeting) (*models.Meeting, error) {
	meeting.ID = uuid.New()
	meeting.CreatedAt = time.Now()
	meeting.UpdatedAt = time.Now()

	if err := s.DB.WithContext(ctx).Create(meeting).Error; err != nil {
		return nil, err
	}

	// Создаем событие в локальном календаре
	if s.Calendar != nil {
		if err := s.Calendar.SyncMeetingToCalendar(ctx, meeting); err != nil {
			log.Printf("Ошибка создания события в календаре: %v", err)
		}
	}

	return meeting, nil
}

// CreateTaskFromTemplate создает задачу из шаблона
func (s *TaskService) CreateTaskFromTemplate(ctx context.Context, templateID string, userID string, companyID uuid.UUID) (*models.Task, error) {
	// Логика создания задачи из шаблона
	// ... existing code ...

	// Создаем новую задачу
	newTask := &models.Task{
		ID:        uuid.New(),
		Title:     "Задача из шаблона",
		Status:    "pending",
		CreatedBy: userID,
		CompanyID: companyID,
		CreatedAt: time.Now(),
	}

	// Создаем событие в локальном календаре
	if s.Calendar != nil {
		if err := s.Calendar.SyncTaskToCalendar(ctx, newTask); err != nil {
			log.Printf("Ошибка создания события в календаре: %v", err)
		}
	}

	return newTask, nil
}

// CreateLeaveRequest создает запрос на отпуск
func (s *TaskService) CreateLeaveRequest(ctx context.Context, userID string, leaveType string, startDate, endDate time.Time) (*models.LeaveRequest, error) {
	// Здесь должна быть логика создания запроса на отпуск
	request := &models.LeaveRequest{
		ID:        uuid.New(),
		UserID:    userID,
		Type:      leaveType,
		StartDate: startDate,
		EndDate:   endDate,
		Status:    "pending",
		CompanyID: uuid.Nil, // Нужно получить из контекста
	}

	if err := s.DB.WithContext(ctx).Create(request).Error; err != nil {
		return nil, err
	}

	return request, nil
}

// GetSalesAnalytics получает аналитику продаж
func (s *TaskService) GetSalesAnalytics(ctx context.Context, userID string, period string) (map[string]interface{}, error) {
	// Здесь должна быть логика получения аналитики продаж
	return map[string]interface{}{
		"user_id": userID,
		"period":  period,
		"data":    map[string]interface{}{},
	}, nil
}

// StartTimeTracking начинает отслеживание времени
func (s *TaskService) StartTimeTracking(ctx context.Context, userID string, taskID string) (*models.TimeEntry, error) {
	// Здесь должна быть логика начала отслеживания времени
	taskUUID, err := uuid.Parse(taskID)
	if err != nil {
		return nil, err
	}

	entry := &models.TimeEntry{
		ID:        uuid.New(),
		UserID:    userID,
		TaskID:    taskUUID,
		StartTime: time.Now(),
	}

	if err := s.DB.WithContext(ctx).Create(entry).Error; err != nil {
		return nil, err
	}

	return entry, nil
}

// StopTimeTracking останавливает отслеживание времени
func (s *TaskService) StopTimeTracking(ctx context.Context, entryID string) (*models.TimeEntry, error) {
	// Здесь должна быть логика остановки отслеживания времени
	entryUUID, err := uuid.Parse(entryID)
	if err != nil {
		return nil, err
	}

	var entry models.TimeEntry
	if err := s.DB.WithContext(ctx).Where("id = ?", entryUUID).First(&entry).Error; err != nil {
		return nil, err
	}

	entry.EndTime = time.Now()
	entry.Duration = int(time.Since(entry.StartTime).Minutes())

	if err := s.DB.WithContext(ctx).Save(&entry).Error; err != nil {
		return nil, err
	}

	return &entry, nil
}

// GetActiveTimeEntries получает активные записи времени
func (s *TaskService) GetActiveTimeEntries(ctx context.Context, userID string) ([]models.TimeEntry, error) {
	var entries []models.TimeEntry
	if err := s.DB.WithContext(ctx).
		Where("user_id = ? AND end_time IS NULL", userID).
		Find(&entries).Error; err != nil {
		return nil, err
	}
	return entries, nil
}

// CalculateTotalActiveTime вычисляет общее активное время
func (s *TaskService) CalculateTotalActiveTime(ctx context.Context, userID string) (int, error) {
	var totalMinutes int64
	if err := s.DB.WithContext(ctx).
		Model(&models.TimeEntry{}).
		Where("user_id = ? AND duration > 0", userID).
		Select("COALESCE(SUM(duration), 0)").
		Scan(&totalMinutes).Error; err != nil {
		return 0, err
	}
	return int(totalMinutes), nil
}

// GetTimeReport получает отчет по времени
func (s *TaskService) GetTimeReport(ctx context.Context, userID string, period string) (map[string]interface{}, error) {
	// Здесь должна быть логика получения отчета по времени
	return map[string]interface{}{
		"user_id": userID,
		"period":  period,
		"data":    map[string]interface{}{},
	}, nil
}

// CalculatePayroll вычисляет зарплату
func (s *TaskService) CalculatePayroll(ctx context.Context, userID string, period string) (*models.Payroll, error) {
	// Здесь должна быть логика вычисления зарплаты
	payroll := &models.Payroll{
		ID:         uuid.New(),
		UserID:     userID,
		Period:     period,
		Hours:      0,
		Rate:       0,
		TaxRate:    0,
		NetSalary:  0,
		Calculated: time.Now(),
		CompanyID:  uuid.Nil, // Нужно получить из контекста
	}
	return payroll, nil
}

// GetAggregatePayroll получает агрегированную зарплату
func (s *TaskService) GetAggregatePayroll(ctx context.Context, userID string, period string) (map[string]interface{}, error) {
	// Здесь должна быть логика получения агрегированной зарплаты
	return map[string]interface{}{
		"user_id": userID,
		"period":  period,
		"data":    map[string]interface{}{},
	}, nil
}

// CreateOnboardingChecklist создает чек-лист онбординга
func (s *TaskService) CreateOnboardingChecklist(ctx context.Context, userID string) (*models.OnboardingChecklist, error) {
	// Здесь должна быть логика создания чек-листа онбординга
	checklist := &models.OnboardingChecklist{
		ID:        uuid.New(),
		UserID:    userID,
		TaskTitle: "Онбординг нового сотрудника",
		Completed: false,
		DueDate:   time.Now().AddDate(0, 0, 7),
	}
	return checklist, nil
}

// CreateRecurringTask создает повторяющуюся задачу
func (s *TaskService) CreateRecurringTask(ctx context.Context, userID string, task *models.Task, recurrence string) (*models.Task, error) {
	// Здесь должна быть логика создания повторяющейся задачи
	task.ID = uuid.New()
	task.CreatedBy = userID
	task.CreatedAt = time.Now()

	if err := s.DB.WithContext(ctx).Create(task).Error; err != nil {
		return nil, err
	}

	return task, nil
}

// GetProductivityReport получает отчет по продуктивности
func (s *TaskService) GetProductivityReport(ctx context.Context, userID string, period string) (map[string]interface{}, error) {
	// Здесь должна быть логика получения отчета по продуктивности
	return map[string]interface{}{
		"user_id": userID,
		"period":  period,
		"data":    map[string]interface{}{},
	}, nil
}

// CreateTaxReminders создает напоминания о налогах
func (s *TaskService) CreateTaxReminders(ctx context.Context, userID string, companyType string) error {
	// Здесь должна быть логика создания напоминаний о налогах
	return nil
}

// Recurrence структура для повторяющихся задач
type Recurrence struct {
	Interval string    `json:"interval"`
	Until    time.Time `json:"until"`
}
