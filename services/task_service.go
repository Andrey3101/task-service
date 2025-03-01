package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/andre/task-service/config"
	"github.com/andre/task-service/models"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/streadway/amqp"
	"gorm.io/gorm"
)

type TaskService struct {
	DB             *gorm.DB
	Redis          *redis.Client
	RabbitMQ       *amqp.Channel
	GoogleCalendar *GoogleCalendarService
}

type TaskHistory struct {
	HistoryID uuid.UUID `json:"history_id" gorm:"type:uuid;primaryKey"`
	TaskID    uuid.UUID `json:"task_id" gorm:"not null"`
	Field     string    `json:"field" gorm:"not null"`
	OldValue  string    `json:"old_value"`
	NewValue  string    `json:"new_value"`
	ChangedAt time.Time `json:"changed_at"`
}

type Report struct {
	ID        uuid.UUID `json:"report_id" gorm:"type:uuid;primaryKey"`
	CompanyID uuid.UUID `json:"company_id"`
	Period    string    `json:"period"`
	Data      string    `json:"data"`
	CreatedAt time.Time `json:"created_at"`
}

type Dashboard struct {
	EmployeeLoad []EmployeeLoad `json:"employee_load"`
	TaskCount    int            `json:"task_count"`
	OverdueTasks int            `json:"overdue_tasks"`
}

type EmployeeLoad struct {
	UserID        string `json:"user_id"`
	TaskCount     int    `json:"task_count"`
	TotalPriority int    `json:"total_priority"`
}

type Suggestion struct {
	Text   string                 `json:"text"`
	Action map[string]interface{} `json:"action"`
}

func NewTaskService(db *gorm.DB, redis *redis.Client, rabbitMQ *amqp.Channel, cfg config.Config) *TaskService {
	return &TaskService{
		DB:             db,
		Redis:          redis,
		RabbitMQ:       rabbitMQ,
		GoogleCalendar: NewGoogleCalendarService(cfg, redis),
	}
}

func (s *TaskService) CreateTask(ctx context.Context, userID string, task *models.Task, autoAssign bool) (*models.Task, error) {
	task.CreatedBy = userID
	task.CreatedAt = time.Now()
	if task.Priority == 0 {
		task.Priority = 1 // Минимальный приоритет по умолчанию
	}

	if autoAssign && len(task.AssigneeIDs) == 0 {
		// Логика автоматического распределения может быть добавлена здесь, если требуется
	}

	tx := s.DB.WithContext(ctx).Begin()
	if err := tx.Create(task).Error; err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Model(task).Association("Assignees").Append(task.Assignees); err != nil {
		tx.Rollback()
		return nil, err
	}
	tx.Commit()

	s.logTaskChange(ctx, task.ID, "created", "", task.Title)
	for _, assignee := range task.Assignees {
		s.logTaskChange(ctx, task.ID, "assignee", "", assignee.UserID)
	}

	if s.GoogleCalendar.Client != nil {
		eventID, err := s.GoogleCalendar.CreateEvent(ctx, task.ID.String(), task.Title, task.Description, task.DueDate)
		if err != nil {
			log.Printf("Failed to create Google Calendar event: %v", err)
		} else {
			log.Printf("Created Google Calendar event %s for task %s", eventID, task.ID)
		}
	}

	s.invalidateCache(ctx, "tasks:*")
	event := struct {
		TaskID    string    `json:"task_id"`
		Action    string    `json:"action"`
		CreatedAt time.Time `json:"created_at"`
	}{TaskID: task.ID.String(), Action: "create", CreatedAt: task.CreatedAt}
	eventJSON, _ := json.Marshal(event)
	s.Redis.Publish(ctx, "task_updates", eventJSON)
	return task, nil
}

func (s *TaskService) GetTasks(ctx context.Context, status string, dueDate *time.Time, limit, offset int) ([]models.Task, int, error) {
	cacheKey := fmt.Sprintf("tasks:%s:%s:%d:%d", status, dueDateString(dueDate), limit, offset)
	if cached, err := s.Redis.Get(ctx, cacheKey).Bytes(); err == nil {
		var result struct {
			Tasks []models.Task `json:"tasks"`
			Total int           `json:"total"`
		}
		if err := json.Unmarshal(cached, &result); err == nil {
			return result.Tasks, result.Total, nil
		}
	}

	query := s.DB.WithContext(ctx).Limit(limit).Offset(offset).Preload("Assignees").Preload("Comments")
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if dueDate != nil {
		start := dueDate.Truncate(24 * time.Hour)
		end := start.Add(24*time.Hour - time.Second)
		query = query.Where("due_date BETWEEN ? AND ?", start, end)
	}

	var total int64
	if err := query.Model(&models.Task{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var tasks []models.Task
	if err := query.Find(&tasks).Error; err != nil {
		return nil, 0, err
	}

	result := struct {
		Tasks []models.Task `json:"tasks"`
		Total int           `json:"total"`
	}{Tasks: tasks, Total: int(total)}
	if data, err := json.Marshal(result); err == nil {
		s.Redis.Set(ctx, cacheKey, data, 5*time.Minute)
	}

	return tasks, int(total), nil
}

func (s *TaskService) GetTask(ctx context.Context, taskID uuid.UUID) (*models.Task, error) {
	var task models.Task
	if err := s.DB.WithContext(ctx).Preload("Assignees").Preload("Comments").First(&task, "id = ?", taskID).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *TaskService) UpdateTask(ctx context.Context, userID string, task *models.Task) (*models.Task, error) {
	var existingTask models.Task
	if err := s.DB.WithContext(ctx).First(&existingTask, "id = ?", task.ID).Error; err != nil {
		return nil, err
	}
	if existingTask.Title != task.Title {
		s.logTaskChange(ctx, task.ID, "title", existingTask.Title, task.Title)
	}
	if existingTask.Status != task.Status {
		s.logTaskChange(ctx, task.ID, "status", existingTask.Status, task.Status)
	}
	if existingTask.Priority != task.Priority {
		s.logTaskChange(ctx, task.ID, "priority", fmt.Sprintf("%d", existingTask.Priority), fmt.Sprintf("%d", task.Priority))
	}

	taskData, _ := json.Marshal(task)
	if err := s.RabbitMQ.Publish("", "tasks_queue", false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        taskData,
	}); err != nil {
		return nil, err
	}

	if s.GoogleCalendar.Client != nil {
		if eventID, err := s.Redis.Get(ctx, "calendar_event:"+task.ID.String()).Result(); err == nil {
			err = s.GoogleCalendar.UpdateEvent(ctx, task.ID.String(), eventID, task.Title, task.Description, task.DueDate)
			if err != nil {
				log.Printf("Failed to update Google Calendar event: %v", err)
			}
		}
	}

	s.invalidateCache(ctx, "tasks:*")
	event := struct {
		TaskID    string    `json:"task_id"`
		Action    string    `json:"action"`
		CreatedAt time.Time `json:"created_at"`
	}{TaskID: task.ID.String(), Action: "update", CreatedAt: task.CreatedAt}
	eventJSON, _ := json.Marshal(event)
	s.Redis.Publish(ctx, "task_updates", eventJSON)
	return task, nil
}

func (s *TaskService) DeleteTask(ctx context.Context, taskID uuid.UUID, userID string) error {
	var task models.Task
	if err := s.DB.WithContext(ctx).First(&task, "id = ?", taskID).Error; err != nil {
		return err
	}
	s.logTaskChange(ctx, taskID, "deleted", task.Title, "")

	deleteData := map[string]interface{}{
		"task_id": taskID.String(),
		"user_id": userID,
	}
	data, _ := json.Marshal(deleteData)
	if err := s.RabbitMQ.Publish("", "tasks_queue", false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        data,
	}); err != nil {
		return err
	}

	if s.GoogleCalendar.Client != nil {
		if eventID, err := s.Redis.Get(ctx, "calendar_event:"+taskID.String()).Result(); err == nil {
			err = s.GoogleCalendar.DeleteEvent(ctx, taskID.String(), eventID)
			if err != nil {
				log.Printf("Failed to delete Google Calendar event: %v", err)
			}
		}
	}

	s.invalidateCache(ctx, "tasks:*")
	event := struct {
		TaskID    string    `json:"task_id"`
		Action    string    `json:"action"`
		CreatedAt time.Time `json:"created_at"`
	}{TaskID: taskID.String(), Action: "delete", CreatedAt: task.CreatedAt}
	eventJSON, _ := json.Marshal(event)
	s.Redis.Publish(ctx, "task_updates", eventJSON)
	return nil
}

func (s *TaskService) AddComment(ctx context.Context, taskID uuid.UUID, userID, text string) (*models.Comment, error) {
	comment := &models.Comment{
		ID:        uuid.New(),
		TaskID:    taskID,
		UserID:    userID,
		Text:      text,
		CreatedAt: time.Now(),
	}
	if err := s.DB.WithContext(ctx).Create(comment).Error; err != nil {
		return nil, err
	}
	s.logTaskChange(ctx, taskID, "comment", "", text)
	s.invalidateCache(ctx, "tasks:*")
	event := struct {
		TaskID    string          `json:"task_id"`
		Action    string          `json:"action"`
		Comment   *models.Comment `json:"comment"`
		CreatedAt time.Time       `json:"created_at"`
	}{TaskID: taskID.String(), Action: "comment", Comment: comment, CreatedAt: time.Now()}
	eventJSON, _ := json.Marshal(event)
	s.Redis.Publish(ctx, "task_updates", eventJSON)
	return comment, nil
}

func (s *TaskService) AddAssignee(ctx context.Context, taskID uuid.UUID, userID string) error {
	assignee := models.Assignee{UserID: userID}
	if err := s.DB.WithContext(ctx).Model(&models.Task{ID: taskID}).Association("Assignees").Append(&assignee); err != nil {
		return err
	}
	s.logTaskChange(ctx, taskID, "assignee", "", userID)
	s.invalidateCache(ctx, "tasks:*")
	return nil
}

func (s *TaskService) invalidateCache(ctx context.Context, pattern string) {
	keys, _ := s.Redis.Keys(ctx, pattern).Result()
	for _, key := range keys {
		s.Redis.Del(ctx, key)
	}
}

func dueDateString(dueDate *time.Time) string {
	if dueDate == nil {
		return ""
	}
	return dueDate.Format("2006-01-02")
}

func (s *TaskService) logTaskChange(ctx context.Context, taskID uuid.UUID, field, oldValue, newValue string) {
	history := &TaskHistory{
		HistoryID: uuid.New(),
		TaskID:    taskID,
		Field:     field,
		OldValue:  oldValue,
		NewValue:  newValue,
		ChangedAt: time.Now(),
	}
	s.DB.WithContext(ctx).Create(history)
}

func (s *TaskService) GenerateReport(ctx context.Context, userID, period string) (*Report, error) {
	var role string
	if err := s.DB.WithContext(ctx).Raw("SELECT role FROM users WHERE user_id = ?", userID).Scan(&role).Error; err != nil {
		return nil, fmt.Errorf("user not found: %v", err)
	}
	if role != "admin" && role != "manager" {
		return nil, fmt.Errorf("only admins and managers can generate reports")
	}

	var companyID uuid.UUID
	s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID)

	var tasks []models.Task
	s.DB.WithContext(ctx).Where("created_by IN (SELECT user_id FROM users WHERE company_id = ?)", companyID).Find(&tasks)

	reportData := map[string]interface{}{
		"period":          period,
		"tasks_completed": len(tasks),
	}
	data, _ := json.Marshal(reportData)
	report := &Report{
		ID:        uuid.New(),
		CompanyID: companyID,
		Period:    period,
		Data:      string(data),
		CreatedAt: time.Now(),
	}
	s.DB.WithContext(ctx).Create(report)
	return report, nil
}

func (s *TaskService) GetReports(ctx context.Context, userID string) ([]Report, error) {
	var role string
	if err := s.DB.WithContext(ctx).Raw("SELECT role FROM users WHERE user_id = ?", userID).Scan(&role).Error; err != nil {
		return nil, fmt.Errorf("user not found: %v", err)
	}
	if role != "admin" && role != "manager" {
		return nil, fmt.Errorf("only admins and managers can view reports")
	}
	var companyID uuid.UUID
	s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID)

	var reports []Report
	s.DB.WithContext(ctx).Where("company_id = ?", companyID).Find(&reports)
	return reports, nil
}

func (s *TaskService) CreateTaskWithPrediction(ctx context.Context, userID string, text string, confirmSuggestion bool, suggestionAction map[string]interface{}) (*models.Task, error) {
	if confirmSuggestion && suggestionAction != nil {
		task := &models.Task{
			ID:        uuid.New(),
			Title:     suggestionAction["title"].(string),
			CreatedBy: userID,
			CreatedAt: time.Now(),
			Status:    "pending",
		}
		if dueDate, ok := suggestionAction["due_date"].(string); ok && dueDate == "tomorrow" {
			task.DueDate = time.Now().Add(24 * time.Hour)
		}
		if assignee, ok := suggestionAction["assignee"].(string); ok {
			task.AssigneeIDs = []string{assignee}
		}
		return s.CreateTask(ctx, userID, task, false)
	}
	task := &models.Task{ID: uuid.New(), Title: text, CreatedBy: userID, CreatedAt: time.Now(), Status: "pending"}
	return s.CreateTask(ctx, userID, task, true)
}

func (s *TaskService) GetDashboard(ctx context.Context, userID string, filters map[string]string) (*Dashboard, error) {
	var role string
	if err := s.DB.WithContext(ctx).Raw("SELECT role FROM users WHERE user_id = ?", userID).Scan(&role).Error; err != nil {
		return nil, fmt.Errorf("user not found: %v", err)
	}
	if role != "admin" {
		return nil, fmt.Errorf("only admins can view dashboard")
	}
	var companyID uuid.UUID
	s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID)

	query := s.DB.WithContext(ctx).Model(&models.Task{}).Where("created_by IN (SELECT user_id FROM users WHERE company_id = ?)", companyID)
	if date, ok := filters["date"]; ok {
		start, _ := time.Parse("2006-01-02", date)
		end := start.Add(24*time.Hour - time.Second)
		query = query.Where("created_at BETWEEN ? AND ?", start, end)
	}
	if status, ok := filters["status"]; ok {
		query = query.Where("status = ?", status)
	}
	if userIDFilter, ok := filters["user_id"]; ok {
		query = query.Where("id IN (SELECT task_id FROM task_assignees WHERE user_id = ?)", userIDFilter)
	}

	var taskCount int64
	if err := query.Count(&taskCount).Error; err != nil {
		return nil, err
	}

	var overdueTasks int64
	overdueQuery := s.DB.WithContext(ctx).Model(&models.Task{}).Where("due_date < ? AND status != 'completed' AND created_by IN (SELECT user_id FROM users WHERE company_id = ?)", time.Now(), companyID)
	if err := overdueQuery.Count(&overdueTasks).Error; err != nil {
		return nil, err
	}

	var employeeLoad []EmployeeLoad
	loadQuery := `
        SELECT u.user_id, COUNT(ta.task_id) as task_count, COALESCE(SUM(t.priority), 0) as total_priority
        FROM users u
        LEFT JOIN task_assignees ta ON u.user_id = ta.user_id
        LEFT JOIN tasks t ON ta.task_id = t.id
        WHERE u.company_id = ? AND u.role = 'user'
        GROUP BY u.user_id
    `
	s.DB.WithContext(ctx).Raw(loadQuery, companyID).Scan(&employeeLoad)

	return &Dashboard{
		EmployeeLoad: employeeLoad,
		TaskCount:    int(taskCount),
		OverdueTasks: int(overdueTasks),
	}, nil
}

func (s *TaskService) GetMyTasks(ctx context.Context, userID string) ([]models.Task, error) {
	var tasks []models.Task
	if err := s.DB.WithContext(ctx).Preload("Assignees").Preload("Comments").Where("id IN (SELECT task_id FROM task_assignees WHERE user_id = ?)", userID).Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

func (s *TaskService) GetTaskSuggestions(ctx context.Context, userID, text string) ([]Suggestion, error) {
	var companyID uuid.UUID
	if err := s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID).Error; err != nil {
		return nil, fmt.Errorf("user not found: %v", err)
	}

	suggestions := []Suggestion{}
	if strings.Contains(strings.ToLower(text), "доставка") {
		var leastLoadedUser struct {
			UserID string
		}
		s.DB.WithContext(ctx).Raw(`
            SELECT u.user_id
            FROM users u
            LEFT JOIN task_assignees ta ON u.user_id = ta.user_id
            WHERE u.company_id = ? AND u.role = 'user'
            GROUP BY u.user_id
            ORDER BY COUNT(ta.task_id) ASC
            LIMIT 1
        `, companyID).Scan(&leastLoadedUser)

		suggestions = append(suggestions, Suggestion{
			Text:   "Назначить задачу на сотрудника с меньшей нагрузкой?",
			Action: map[string]interface{}{"assignee": leastLoadedUser.UserID},
		})
	}
	if strings.Contains(strings.ToLower(text), "на завтра") {
		suggestions = append(suggestions, Suggestion{
			Text:   "Добавить напоминание на завтра?",
			Action: map[string]interface{}{"due_date": "tomorrow"},
		})
	}
	return suggestions, nil
}

func (s *TaskService) BulkAssignTasks(ctx context.Context, userID string, taskIDs []uuid.UUID, assigneeID string) error {
	var role string
	if err := s.DB.WithContext(ctx).Raw("SELECT role FROM users WHERE user_id = ?", userID).Scan(&role).Error; err != nil {
		return fmt.Errorf("user not found: %v", err)
	}
	if role != "admin" {
		return fmt.Errorf("only admins can bulk assign tasks")
	}
	tx := s.DB.WithContext(ctx).Begin()
	for _, taskID := range taskIDs {
		assignee := models.Assignee{UserID: assigneeID}
		if err := tx.Model(&models.Task{ID: taskID}).Association("Assignees").Append(&assignee); err != nil {
			tx.Rollback()
			return err
		}
		s.logTaskChange(ctx, taskID, "assignee", "", assigneeID)
	}
	tx.Commit()
	s.invalidateCache(ctx, "tasks:*")
	return nil
}
