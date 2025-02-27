package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"tasks-service/config"
	"tasks-service/models"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/streadway/amqp"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TaskService управляет задачами с учетом шардирования и партиционирования
type TaskService struct {
	DB             *gorm.DB
	Redis          *redis.Client
	RabbitMQ       *amqp.Channel
	AuthDBShards   map[int]*gorm.DB
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

// GoogleCalendarService для работы с Google Calendar API
type GoogleCalendarService struct {
	Config *oauth2.Config
	Client *calendar.Service
	Redis  *redis.Client
}

// NewGoogleCalendarService инициализирует сервис Google Calendar
func NewGoogleCalendarService(cfg config.Config, redisClient *redis.Client) *GoogleCalendarService {
	config := &oauth2.Config{
		ClientID:     cfg.GoogleClientID,
		ClientSecret: cfg.GoogleClientSecret,
		RedirectURL:  cfg.GoogleRedirectURI,
		Scopes:       []string{calendar.CalendarScope},
		Endpoint:     google.Endpoint,
	}
	return &GoogleCalendarService{Config: config, Redis: redisClient}
}

func (s *GoogleCalendarService) GetAuthURL() string {
	return s.Config.AuthCodeURL("state", oauth2.AccessTypeOffline)
}

func (s *GoogleCalendarService) Authenticate(ctx context.Context, code string) error {
	token, err := s.Config.Exchange(ctx, code)
	if err != nil {
		return err
	}
	client, err := calendar.NewService(ctx, option.WithTokenSource(s.Config.TokenSource(ctx, token)))
	if err != nil {
		return err
	}
	s.Client = client
	tokenJSON, _ := json.Marshal(token)
	s.Redis.Set(ctx, "google_calendar_token", tokenJSON, 0)
	return nil
}

func (s *GoogleCalendarService) LoadToken(ctx context.Context) error {
	tokenJSON, err := s.Redis.Get(ctx, "google_calendar_token").Bytes()
	if err != nil {
		return err
	}
	var token oauth2.Token
	if err := json.Unmarshal(tokenJSON, &token); err != nil {
		return err
	}
	client, err := calendar.NewService(ctx, option.WithTokenSource(s.Config.TokenSource(ctx, &token)))
	if err != nil {
		return err
	}
	s.Client = client
	return nil
}

func (s *GoogleCalendarService) CreateEvent(ctx context.Context, taskID, title, description string, dueDate time.Time) (string, error) {
	if s.Client == nil {
		if err := s.LoadToken(ctx); err != nil {
			return "", err
		}
	}
	event := &calendar.Event{
		Summary:     title,
		Description: description,
		Start: &calendar.EventDateTime{
			DateTime: dueDate.Format(time.RFC3339),
			TimeZone: "UTC",
		},
		End: &calendar.EventDateTime{
			DateTime: dueDate.Add(time.Hour).Format(time.RFC3339),
			TimeZone: "UTC",
		},
		Reminders: &calendar.EventReminders{
			UseDefault: false,
			Overrides:  []*calendar.EventReminder{{Method: "email", Minutes: 15}},
		},
	}
	createdEvent, err := s.Client.Events.Insert("primary", event).Do()
	if err != nil {
		return "", err
	}
	s.Redis.Set(ctx, "calendar_event:"+taskID, createdEvent.Id, 0)
	return createdEvent.Id, nil
}

func (s *GoogleCalendarService) UpdateEvent(ctx context.Context, taskID, eventID, title, description string, dueDate time.Time) error {
	if s.Client == nil {
		if err := s.LoadToken(ctx); err != nil {
			return err
		}
	}
	event, err := s.Client.Events.Get("primary", eventID).Do()
	if err != nil {
		return err
	}
	event.Summary = title
	event.Description = description
	event.Start = &calendar.EventDateTime{DateTime: dueDate.Format(time.RFC3339), TimeZone: "UTC"}
	event.End = &calendar.EventDateTime{DateTime: dueDate.Add(time.Hour).Format(time.RFC3339), TimeZone: "UTC"}
	_, err = s.Client.Events.Update("primary", eventID, event).Do()
	return err
}

func (s *GoogleCalendarService) DeleteEvent(ctx context.Context, taskID, eventID string) error {
	if s.Client == nil {
		if err := s.LoadToken(ctx); err != nil {
			return err
		}
	}
	err := s.Client.Events.Delete("primary", eventID).Do()
	if err == nil {
		s.Redis.Del(ctx, "calendar_event:"+taskID)
	}
	return err
}

// GetCalendarEvents возвращает список событий из Google Calendar
func (s *GoogleCalendarService) GetCalendarEvents(ctx context.Context) ([]*calendar.Event, error) {
	if s.Client == nil {
		if err := s.LoadToken(ctx); err != nil {
			return nil, fmt.Errorf("failed to load Google Calendar token: %v", err)
		}
	}
	events, err := s.Client.Events.List("primary").TimeMin(time.Now().Add(-7 * 24 * time.Hour).Format(time.RFC3339)).TimeMax(time.Now().Add(7 * 24 * time.Hour).Format(time.RFC3339)).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch events: %v", err)
	}
	return events.Items, nil
}

// CreateCalendarEvent создаёт новое событие в Google Calendar
func (s *GoogleCalendarService) CreateCalendarEvent(ctx context.Context, summary, description, startTime string) (string, error) {
	if s.Client == nil {
		if err := s.LoadToken(ctx); err != nil {
			return "", fmt.Errorf("failed to load Google Calendar token: %v", err)
		}
	}
	start, err := time.Parse(time.RFC3339, startTime)
	if err != nil {
		return "", fmt.Errorf("invalid start time format: %v", err)
	}
	event := &calendar.Event{
		Summary:     summary,
		Description: description,
		Start: &calendar.EventDateTime{
			DateTime: start.Format(time.RFC3339),
			TimeZone: "UTC",
		},
		End: &calendar.EventDateTime{
			DateTime: start.Add(time.Hour).Format(time.RFC3339), // +1 час по умолчанию
			TimeZone: "UTC",
		},
		Reminders: &calendar.EventReminders{
			UseDefault: false,
			Overrides:  []*calendar.EventReminder{{Method: "email", Minutes: 15}},
		},
	}
	createdEvent, err := s.Client.Events.Insert("primary", event).Do()
	if err != nil {
		return "", fmt.Errorf("failed to create event: %v", err)
	}
	return createdEvent.Id, nil
}

// UpdateCalendarEvent обновляет существующее событие в Google Calendar
func (s *GoogleCalendarService) UpdateCalendarEvent(ctx context.Context, eventID, summary, description, startTime string) (string, error) {
	if s.Client == nil {
		if err := s.LoadToken(ctx); err != nil {
			return "", fmt.Errorf("failed to load Google Calendar token: %v", err)
		}
	}
	start, err := time.Parse(time.RFC3339, startTime)
	if err != nil {
		return "", fmt.Errorf("invalid start time format: %v", err)
	}
	event, err := s.Client.Events.Get("primary", eventID).Do()
	if err != nil {
		return "", fmt.Errorf("failed to get event: %v", err)
	}
	event.Summary = summary
	event.Description = description
	event.Start = &calendar.EventDateTime{DateTime: start.Format(time.RFC3339), TimeZone: "UTC"}
	event.End = &calendar.EventDateTime{DateTime: start.Add(time.Hour).Format(time.RFC3339), TimeZone: "UTC"}
	updatedEvent, err := s.Client.Events.Update("primary", eventID, event).Do()
	if err != nil {
		return "", fmt.Errorf("failed to update event: %v", err)
	}
	return updatedEvent.Id, nil
}

func (s *GoogleCalendarService) DeleteCalendarEvent(ctx context.Context, eventID string) error {
	if s.Client == nil {
		if err := s.LoadToken(ctx); err != nil {
			return fmt.Errorf("failed to load Google Calendar token: %v", err)
		}
	}
	err := s.Client.Events.Delete("primary", eventID).Do()
	if err != nil {
		return fmt.Errorf("failed to delete event: %v", err)
	}
	return nil
}

// NewTaskService инициализирует сервис с шардированными подключениями и Google Calendar
func NewTaskService(db *gorm.DB, redis *redis.Client, rabbitMQ *amqp.Channel, cfg config.Config) *TaskService {
	authDBShards := make(map[int]*gorm.DB)
	for i := 0; i < 2; i++ {
		dsn := fmt.Sprintf("postgres://user:password@localhost:5432/tasks?sslmode=disable&search_path=auth_shard_%d", i)
		shardDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
		if err != nil {
			log.Fatalf("Failed to connect to auth_shard_%d: %v", i, err)
		}
		authDBShards[i] = shardDB
	}
	return &TaskService{
		DB:             db,
		Redis:          redis,
		RabbitMQ:       rabbitMQ,
		AuthDBShards:   authDBShards,
		GoogleCalendar: NewGoogleCalendarService(cfg, redis),
	}
}

// getShard определяет шард по company_id
func (s *TaskService) getShard(companyID uuid.UUID) int {
	hash := fnv32a(companyID.String())
	return int(hash % 2)
}

// fnv32a - хэш-функция для определения шарда
func fnv32a(str string) uint32 {
	const (
		offset32 = 2166136261
		prime32  = 16777619
	)
	hash := uint32(offset32)
	for _, c := range str {
		hash ^= uint32(c)
		hash *= prime32
	}
	return hash
}

// CreateTask создает задачу с учетом шардирования, автоматического распределения и Google Calendar
func (s *TaskService) CreateTask(ctx context.Context, userID string, task *models.Task, autoAssign bool) (*models.Task, error) {
	shard := s.determineUserShard(ctx, userID)
	if shard == -1 {
		return nil, fmt.Errorf("user not found")
	}
	var companyID uuid.UUID
	s.AuthDBShards[shard].Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID)

	task.CreatedBy = userID
	task.CreatedAt = time.Now()
	if task.Priority == 0 {
		task.Priority = 1 // Минимальный приоритет по умолчанию
	}

	if autoAssign && len(task.Assignees) == 0 {
		if err := s.distributeTask(ctx, task, companyID); err != nil {
			return nil, err
		}
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

	// Интеграция с Google Calendar
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

// distributeTask распределяет задачу с учетом приоритета
func (s *TaskService) distributeTask(ctx context.Context, task *models.Task, companyID uuid.UUID) error {
	var users []struct {
		UserID        string `gorm:"column:user_id"`
		TaskCount     int    `gorm:"column:task_count"`
		TotalPriority int    `gorm:"column:total_priority"`
	}
	shard := s.getShard(companyID)
	query := `
        SELECT u.user_id, COUNT(ta.task_id) as task_count, COALESCE(SUM(t.priority), 0) as total_priority
        FROM auth_shard_%d.users u
        LEFT JOIN task_assignees ta ON u.user_id = ta.user_id
        LEFT JOIN tasks t ON ta.task_id = t.id
        WHERE u.company_id = ? AND u.role = 'user'
        GROUP BY u.user_id
        ORDER BY total_priority ASC, task_count ASC
        LIMIT 1
    `
	if err := s.AuthDBShards[shard].WithContext(ctx).Raw(fmt.Sprintf(query, shard), companyID).Scan(&users).Error; err != nil {
		return err
	}
	if len(users) == 0 {
		return fmt.Errorf("no available users")
	}
	task.Assignees = []models.Assignee{{UserID: users[0].UserID}}
	return nil
}

// GetTasks возвращает задачи с фильтрацией
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

// GetTask получает задачу по ID
func (s *TaskService) GetTask(ctx context.Context, taskID uuid.UUID) *models.Task {
	var task models.Task
	if err := s.DB.WithContext(ctx).Preload("Assignees").Preload("Comments").First(&task, "id = ?", taskID).Error; err != nil {
		return nil
	}
	return &task
}

// UpdateTask обновляет задачу асинхронно через RabbitMQ и Google Calendar
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

	// Обновление события в Google Calendar
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

// DeleteTask удаляет задачу асинхронно через RabbitMQ и Google Calendar
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

	// Удаление события из Google Calendar
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

// AddComment добавляет комментарий
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

// AddAssignee добавляет исполнителя
func (s *TaskService) AddAssignee(ctx context.Context, taskID uuid.UUID, userID string) error {
	assignee := models.Assignee{UserID: userID}
	if err := s.DB.WithContext(ctx).Model(&models.Task{ID: taskID}).Association("Assignees").Append(&assignee); err != nil {
		return err
	}
	s.logTaskChange(ctx, taskID, "assignee", "", userID)
	s.invalidateCache(ctx, "tasks:*")
	return nil
}

// determineUserShard определяет шард пользователя
func (s *TaskService) determineUserShard(ctx context.Context, userID string) int {
	for shard, db := range s.AuthDBShards {
		var companyID uuid.UUID
		err := db.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID).Error
		if err == nil {
			return shard
		}
	}
	return -1 // Пользователь не найден
}

// invalidateCache очищает кэш
func (s *TaskService) invalidateCache(ctx context.Context, pattern string) {
	keys, _ := s.Redis.Keys(ctx, pattern).Result()
	for _, key := range keys {
		s.Redis.Del(ctx, key)
	}
}

// dueDateString форматирует дату
func dueDateString(dueDate *time.Time) string {
	if dueDate == nil {
		return ""
	}
	return dueDate.Format("2006-01-02")
}

// logTaskChange логирует изменения задачи
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

// GenerateReport генерирует отчет
func (s *TaskService) GenerateReport(ctx context.Context, userID, period string) (*Report, error) {
	shard := s.determineUserShard(ctx, userID)
	if shard == -1 {
		return nil, fmt.Errorf("user not found")
	}
	var role string
	s.AuthDBShards[shard].Raw("SELECT role FROM users WHERE user_id = ?", userID).Scan(&role)
	if role != "admin" && role != "manager" {
		return nil, fmt.Errorf("only admins and managers can generate reports")
	}

	var companyID uuid.UUID
	s.AuthDBShards[shard].Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID)

	var tasks []models.Task
	s.DB.WithContext(ctx).Where("created_by IN (SELECT user_id FROM auth_shard_%d.users WHERE company_id = ?)", shard, companyID).Find(&tasks)

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

// GetReports возвращает список отчетов
func (s *TaskService) GetReports(ctx context.Context, userID string) ([]Report, error) {
	shard := s.determineUserShard(ctx, userID)
	if shard == -1 {
		return nil, fmt.Errorf("user not found")
	}
	var role string
	s.AuthDBShards[shard].Raw("SELECT role FROM users WHERE user_id = ?", userID).Scan(&role)
	if role != "admin" && role != "manager" {
		return nil, fmt.Errorf("only admins and managers can view reports")
	}
	var companyID uuid.UUID
	s.AuthDBShards[shard].Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID)

	var reports []Report
	s.DB.WithContext(ctx).Where("company_id = ?", companyID).Find(&reports)
	return reports, nil
}

// CreateTaskWithPrediction создает задачу с предиктивным вводом
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
			task.Assignees = []models.Assignee{{UserID: assignee}}
		}
		return s.CreateTask(ctx, userID, task, false)
	}
	task := &models.Task{ID: uuid.New(), Title: text, CreatedBy: userID, CreatedAt: time.Now(), Status: "pending"}
	return s.CreateTask(ctx, userID, task, true)
}

// GetDashboard возвращает данные для дашборда с фильтрами
func (s *TaskService) GetDashboard(ctx context.Context, userID string, filters map[string]string) (*Dashboard, error) {
	shard := s.determineUserShard(ctx, userID)
	if shard == -1 {
		return nil, fmt.Errorf("user not found")
	}
	var role string
	s.AuthDBShards[shard].Raw("SELECT role FROM users WHERE user_id = ?", userID).Scan(&role)
	if role != "admin" {
		return nil, fmt.Errorf("only admins can view dashboard")
	}
	var companyID uuid.UUID
	s.AuthDBShards[shard].Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID)

	query := s.DB.WithContext(ctx).Model(&models.Task{}).Where("created_by IN (SELECT user_id FROM auth_shard_%d.users WHERE company_id = ?)", shard, companyID)
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
	overdueQuery := s.DB.WithContext(ctx).Model(&models.Task{}).Where("due_date < ? AND status != 'completed' AND created_by IN (SELECT user_id FROM auth_shard_%d.users WHERE company_id = ?)", time.Now(), shard, companyID)
	if err := overdueQuery.Count(&overdueTasks).Error; err != nil {
		return nil, err
	}

	var employeeLoad []EmployeeLoad
	loadQuery := `
        SELECT u.user_id, COUNT(ta.task_id) as task_count, COALESCE(SUM(t.priority), 0) as total_priority
        FROM auth_shard_%d.users u
        LEFT JOIN task_assignees ta ON u.user_id = ta.user_id
        LEFT JOIN tasks t ON ta.task_id = t.id
        WHERE u.company_id = ? AND u.role = 'user'
        GROUP BY u.user_id
    `
	s.AuthDBShards[shard].WithContext(ctx).Raw(fmt.Sprintf(loadQuery, shard), companyID).Scan(&employeeLoad)

	return &Dashboard{
		EmployeeLoad: employeeLoad,
		TaskCount:    int(taskCount),
		OverdueTasks: int(overdueTasks),
	}, nil
}

// GetMyTasks возвращает задачи пользователя
func (s *TaskService) GetMyTasks(ctx context.Context, userID string) ([]models.Task, error) {
	var tasks []models.Task
	if err := s.DB.WithContext(ctx).Preload("Assignees").Preload("Comments").Where("id IN (SELECT task_id FROM task_assignees WHERE user_id = ?)", userID).Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

// GetTaskSuggestions возвращает контекстные подсказки
func (s *TaskService) GetTaskSuggestions(ctx context.Context, userID, text string) ([]Suggestion, error) {
	shard := s.determineUserShard(ctx, userID)
	if shard == -1 {
		return nil, fmt.Errorf("user not found")
	}
	var companyID uuid.UUID
	s.AuthDBShards[shard].Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID)

	suggestions := []Suggestion{}
	if strings.Contains(strings.ToLower(text), "доставка") {
		var leastLoadedUser struct {
			UserID string
		}
		s.AuthDBShards[shard].Raw(`
            SELECT u.user_id
            FROM auth_shard_%d.users u
            LEFT JOIN task_assignees ta ON u.user_id = ta.user_id
            WHERE u.company_id = ? AND u.role = 'user'
            GROUP BY u.user_id
            ORDER BY COUNT(ta.task_id) ASC
            LIMIT 1
        `, shard, companyID).Scan(&leastLoadedUser)

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

// BulkAssignTasks массово назначает задачи
func (s *TaskService) BulkAssignTasks(ctx context.Context, userID string, taskIDs []uuid.UUID, assigneeID string) error {
	shard := s.determineUserShard(ctx, userID)
	if shard == -1 {
		return fmt.Errorf("user not found")
	}
	var role string
	s.AuthDBShards[shard].Raw("SELECT role FROM users WHERE user_id = ?", userID).Scan(&role)
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
