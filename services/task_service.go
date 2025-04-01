package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/andre/task-service/models"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/streadway/amqp"
	"gorm.io/gorm"
)

type TaskService struct {
	DB             *gorm.DB
	Redis          *redis.Client
	GoogleCalendar *GoogleCalendarService
	RabbitMQConn   *amqp.Connection
	RabbitMQChan   *amqp.Channel
}

type Recurrence struct {
	Interval string    `json:"interval"` // "daily", "weekly", "monthly"
	Until    time.Time `json:"until"`    // До какого времени повторять
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
	EmployeeLoad       []EmployeeLoad `json:"employee_load"`
	TaskCount          int            `json:"task_count"`
	OverdueTasks       int            `json:"overdue_tasks"`
	CompletedTasks     int            `json:"completed_tasks"`
	NewClients         int            `json:"new_clients"`
	TotalAmount        float64        `json:"total_amount"`
	Productivity       float64        `json:"productivity"`
	TasksChange        float64        `json:"tasks_change"`
	ClientsChange      float64        `json:"clients_change"`
	AmountChange       float64        `json:"amount_change"`
	ProductivityChange float64        `json:"productivity_change"`
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

type SalesAnalytics struct {
	TotalTasks     int            `json:"total_tasks"`
	CompletedTasks int            `json:"completed_tasks"`
	ConversionRate float64        `json:"conversion_rate"`
	ChannelStats   map[string]int `json:"channel_stats"`
	Funnel         map[string]int `json:"funnel"`
}

type FinancialEvent struct {
	EventID   uuid.UUID `json:"event_id" gorm:"type:uuid;primaryKey"`
	UserID    string    `json:"user_id" gorm:"not null"`
	Title     string    `json:"title" gorm:"not null"`
	DueDate   time.Time `json:"due_date" gorm:"not null"`
	Amount    float64   `json:"amount"`
	Category  string    `json:"category"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *TaskService) GetSalesAnalytics(ctx context.Context, userID string, period string) (*SalesAnalytics, error) {
	var companyID uuid.UUID
	if err := s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID).Error; err != nil {
		return nil, fmt.Errorf("user not found: %v", err)
	}

	var tasks []models.Task
	query := s.DB.Where("created_by = ?", userID)
	if period != "" {
		switch period {
		case "day":
			query = query.Where("created_at >= ?", time.Now().AddDate(0, 0, -1))
		case "week":
			query = query.Where("created_at >= ?", time.Now().AddDate(0, 0, -7))
		case "month":
			query = query.Where("created_at >= ?", time.Now().AddDate(0, -1, 0))
		}
	}
	if err := query.Find(&tasks).Error; err != nil {
		return nil, err
	}

	total := len(tasks)
	completed := 0
	channelStats := make(map[string]int)
	funnel := make(map[string]int)
	for _, task := range tasks {
		if task.Status == "completed" {
			completed++
		}
		if task.Channel != "" {
			channelStats[task.Channel]++
		} else {
			channelStats["unknown"]++
		}
		funnel[task.Stage]++
	}

	conversionRate := 0.0
	if total > 0 {
		conversionRate = float64(completed) / float64(total) * 100
	}

	return &SalesAnalytics{
		TotalTasks:     total,
		CompletedTasks: completed,
		ConversionRate: conversionRate,
		ChannelStats:   channelStats,
		Funnel:         funnel,
	}, nil
}

func NewTaskService(db *gorm.DB, redisClient *redis.Client, googleCalendar *GoogleCalendarService, rabbitMQURL string) (*TaskService, error) {
	conn, err := amqp.Dial(rabbitMQURL)
	if err != nil {
		return nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, err
	}
	service := &TaskService{
		DB:             db,
		Redis:          redisClient,
		GoogleCalendar: googleCalendar,
		RabbitMQConn:   conn,
		RabbitMQChan:   ch,
	}
	go service.ConsumeRabbitMQ(context.Background())
	return service, nil
}

func (s *TaskService) Close() {
	if s.RabbitMQChan != nil {
		if err := s.RabbitMQChan.Close(); err != nil {
			log.Printf("Error closing RabbitMQ channel: %v", err)
		}
	}
	if s.RabbitMQConn != nil {
		if err := s.RabbitMQConn.Close(); err != nil {
			log.Printf("Error closing RabbitMQ connection: %v", err)
		}
	}
}

func (s *TaskService) CreateTask(ctx context.Context, userID string, task *models.Task, autoAssign bool) (*models.Task, error) {
	defer func() {
		if r := recover(); r != nil {
			stack := make([]byte, 1024)
			runtime.Stack(stack, false)
			log.Printf("Паника в CreateTask: %v\nСтек: %s", r, stack)
		}
	}()

	if task.Title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if !isValidStatus(task.Status) {
		return nil, fmt.Errorf("invalid status: %s", task.Status)
	}

	var companyID uuid.UUID
	if err := s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID).Error; err != nil {
		return nil, fmt.Errorf("failed to get user company: %v", err)
	}

	task.CreatedBy = userID
	task.CreatedAt = time.Now()
	task.CompanyID = companyID
	if task.Priority == 0 {
		task.Priority = 1
	}

	// Преобразование AssigneeIDs в Assignees
	if len(task.AssigneeIDs) > 0 {
		task.Assignees = make([]models.Assignee, len(task.AssigneeIDs))
		for i, id := range task.AssigneeIDs {
			var assigneeCompanyID uuid.UUID
			if err := s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", id).Scan(&assigneeCompanyID).Error; err != nil || assigneeCompanyID != companyID {
				return nil, fmt.Errorf("assignee %s not found in company", id)
			}
			task.Assignees[i] = models.Assignee{UserID: id}
		}
	}

	tx := s.DB.WithContext(ctx).Begin()
	if err := tx.Create(task).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create task: %v", err)
	}
	if len(task.Assignees) > 0 {
		if err := tx.Model(task).Association("Assignees").Append(task.Assignees); err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to assign assignees: %v", err)
		}
	}
	tx.Commit()

	if strings.ToLower(task.Description) == "follow-up" || strings.Contains(strings.ToLower(task.Title), "follow-up") {
		action := map[string]interface{}{
			"type": "generate_proposal",
			"data": map[string]interface{}{
				"template_id": "proposal_v1",
				"client_data": map[string]interface{}{
					"name":        "Клиент из задачи " + task.Title,
					"amount":      10000,
					"description": task.Description,
				},
				"user_id": userID,
				"task_id": task.ID.String(),
			},
		}
		if err := publishToRabbitMQ(s.RabbitMQChan, "document_queue", action); err != nil {
			log.Printf("Ошибка отправки в RabbitMQ: %v", err)
		}
	}

	s.invalidateCache(ctx, "tasks:*")
	publishTaskUpdate(s.Redis, task.ID.String(), "create", task.CreatedAt)
	return task, nil
}

// Вспомогательные функции
func isValidStatus(status string) bool {
	validStatuses := map[string]bool{"pending": true, "in_progress": true, "completed": true}
	return validStatuses[status]
}

func publishToRabbitMQ(ch *amqp.Channel, queue string, data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return ch.Publish("", queue, false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        jsonData,
	})
}

func publishTaskUpdate(redisClient *redis.Client, taskID, action string, createdAt time.Time) {
	event := struct {
		TaskID    string    `json:"task_id"`
		Action    string    `json:"action"`
		CreatedAt time.Time `json:"created_at"`
	}{TaskID: taskID, Action: action, CreatedAt: createdAt}
	eventJSON, _ := json.Marshal(event)
	redisClient.Publish(context.Background(), "task_updates", eventJSON)
}

// CreateRecurringTask создаёт задачу с повторением
func (s *TaskService) CreateRecurringTask(ctx context.Context, userID string, task *models.Task, recurrence *Recurrence) (*models.Task, error) {
	task, err := s.CreateTask(ctx, userID, task, true)
	if err != nil {
		return nil, err
	}

	if recurrence != nil {
		validIntervals := map[string]bool{"daily": true, "weekly": true, "monthly": true}
		if !validIntervals[recurrence.Interval] {
			return nil, fmt.Errorf("invalid recurrence interval: %s", recurrence.Interval)
		}
		job := map[string]interface{}{
			"type":        "create_recurring_task",
			"task_id":     task.ID.String(),
			"user_id":     userID,
			"interval":    recurrence.Interval,
			"until":       recurrence.Until.Format(time.RFC3339),
			"title":       task.Title,
			"description": task.Description,
			"priority":    task.Priority,
		}
		jobJSON, _ := json.Marshal(job)
		if err := s.RabbitMQChan.Publish("", "recurring_tasks", false, false, amqp.Publishing{
			ContentType: "application/json",
			Body:        jobJSON,
		}); err != nil {
			log.Printf("Ошибка отправки в очередь recurring_tasks: %v", err)
		}
	}
	return task, nil
}

// GetProductivityReport возвращает отчёт о продуктивности за период
func (s *TaskService) GetProductivityReport(ctx context.Context, userID, period string) (map[string]interface{}, error) {
	cacheKey := fmt.Sprintf("productivity:%s:%s", userID, period)
	if cached, err := s.Redis.Get(ctx, cacheKey).Bytes(); err == nil {
		var report map[string]interface{}
		if err := json.Unmarshal(cached, &report); err == nil {
			log.Printf("GetProductivityReport: данные из кэша для %s", cacheKey)
			return report, nil
		}
	}

	var companyID uuid.UUID
	if err := s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID).Error; err != nil {
		return nil, fmt.Errorf("user not found: %v", err)
	}

	var entries []models.TimeEntry
	query := s.DB.WithContext(ctx).Where("user_id = ?", userID)
	switch period {
	case "day":
		query = query.Where("start_time >= ?", time.Now().Truncate(24*time.Hour))
	case "week":
		query = query.Where("start_time >= ?", time.Now().AddDate(0, 0, -7))
	case "month":
		query = query.Where("start_time >= ?", time.Now().AddDate(0, -1, 0))
	default:
		return nil, fmt.Errorf("invalid period: %s", period)
	}
	if err := query.Find(&entries).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch time entries: %v", err)
	}

	totalMinutes := 0
	taskTimes := make(map[string]int)
	for _, entry := range entries {
		duration := entry.Duration
		if entry.EndTime.IsZero() {
			duration = int(time.Now().Sub(entry.StartTime).Minutes())
		}
		totalMinutes += duration
		taskTimes[entry.TaskID.String()] += duration
	}

	report := map[string]interface{}{
		"total_minutes": totalMinutes,
		"task_times":    taskTimes,
	}
	if data, err := json.Marshal(report); err == nil {
		s.Redis.Set(ctx, cacheKey, data, 10*time.Minute)
		log.Printf("GetProductivityReport: данные сохранены в кэш для %s", cacheKey)
	}
	return report, nil
}

// CreateTaxReminders создаёт напоминания о налогах и отчётах
func (s *TaskService) CreateTaxReminders(ctx context.Context, userID, companyType string) error {
	taxDates := map[string][]struct {
		Title   string
		DueDate time.Time
	}{
		"self-employed": {
			{"Подать декларацию за квартал", time.Now().AddDate(0, 3, 0)},
			{"Оплатить налог за квартал", time.Now().AddDate(0, 3, 10)},
		},
		"ip": {
			{"Оплатить НДС", time.Now().AddDate(0, 1, 0)},
			{"Подать годовую декларацию", time.Now().AddDate(1, 0, 0)},
		},
	}

	reminders, exists := taxDates[companyType]
	if !exists {
		log.Printf("Неподдерживаемый тип компании: %s для userID: %s", companyType, userID)
		return fmt.Errorf("unsupported company type: %s", companyType)
	}

	for _, reminder := range reminders {
		task := &models.Task{
			ID:        uuid.New(),
			Title:     reminder.Title,
			CreatedBy: userID,
			CreatedAt: time.Now(),
			DueDate:   reminder.DueDate,
			Status:    "pending",
			Priority:  2, // Средний приоритет
		}
		if _, err := s.CreateTask(ctx, userID, task, true); err != nil {
			log.Printf("Ошибка создания задачи напоминания о налогах: %v", err)
			return err
		}
	}
	return nil
}

func (s *TaskService) GetTasks(ctx context.Context, status string, dueDate *time.Time, priority, assigneeID, fields string, limit, offset int) ([]models.Task, int, error) {
	defer func() {
		if r := recover(); r != nil {
			stack := make([]byte, 1024)
			runtime.Stack(stack, false)
			log.Printf("Panic in GetTasks: %v\nStack: %s", r, stack)
		}
	}()

	userID := ctx.Value("user_id").(string)
	var companyID uuid.UUID
	if err := s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to get user company: %v", err)
	}

	if assigneeID != "" {
		var assigneeCompanyID uuid.UUID
		if err := s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", assigneeID).Scan(&assigneeCompanyID).Error; err != nil || assigneeCompanyID != companyID {
			return nil, 0, fmt.Errorf("assignee %s not found in company", assigneeID)
		}
	}

	cacheKey := fmt.Sprintf("tasks:%s:%s:%s:%s:%s:%d:%d", status, dueDateString(dueDate), priority, assigneeID, fields, limit, offset)
	if cached, err := s.Redis.Get(ctx, cacheKey).Bytes(); err == nil {
		var result struct {
			Tasks []models.Task `json:"tasks"`
			Total int           `json:"total"`
		}
		if err := json.Unmarshal(cached, &result); err == nil {
			return result.Tasks, result.Total, nil
		}
	}

	query := s.DB.WithContext(ctx).
		Where("created_by IN (SELECT user_id FROM users WHERE company_id = ?)", companyID).
		Limit(limit).
		Offset(offset)

	if strings.Contains(fields, "assignees") {
		query = query.Preload("Assignees")
	}
	if strings.Contains(fields, "comments") {
		query = query.Preload("Comments")
	}

	if status != "" && !isValidStatus(status) {
		return nil, 0, fmt.Errorf("invalid status: %s", status)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if dueDate != nil {
		start := dueDate.Truncate(24 * time.Hour)
		end := start.Add(24*time.Hour - time.Second)
		query = query.Where("due_date BETWEEN ? AND ?", start, end)
	}
	if priority != "" {
		if p, err := strconv.Atoi(priority); err != nil || p < 0 {
			return nil, 0, fmt.Errorf("invalid priority: %s", priority)
		}
		query = query.Where("priority = ?", priority)
	}
	if assigneeID != "" {
		query = query.Where("id IN (SELECT task_id FROM task_assignees WHERE user_id = ?)", assigneeID)
	}

	var total int64
	if err := query.Model(&models.Task{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count tasks: %v", err)
	}

	var tasks []models.Task
	if err := query.Find(&tasks).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to fetch tasks: %v", err)
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
	userID := ctx.Value("user_id").(string)
	var companyID uuid.UUID
	if err := s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID).Error; err != nil {
		return nil, fmt.Errorf("failed to get user company: %v", err)
	}

	var task models.Task
	if err := s.DB.WithContext(ctx).
		Preload("Assignees").
		Preload("Comments").
		Where("id = ? AND company_id = ?", taskID, companyID).
		First(&task).Error; err != nil {
		return nil, fmt.Errorf("task not found or permission denied: %v", err)
	}
	return &task, nil
}

func (s *TaskService) UpdateTask(ctx context.Context, userID string, task *models.Task) (*models.Task, error) {
	defer func() {
		if r := recover(); r != nil {
			stack := make([]byte, 1024)
			runtime.Stack(stack, false)
			log.Printf("Паника в UpdateTask: %v\nСтек: %s", r, stack)
		}
	}()

	if task.ID == uuid.Nil {
		return nil, fmt.Errorf("task ID is required")
	}
	if task.Title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if !isValidStatus(task.Status) {
		return nil, fmt.Errorf("invalid status: %s", task.Status)
	}

	var existingTask models.Task
	if err := s.DB.WithContext(ctx).First(&existingTask, "id = ?", task.ID).Error; err != nil {
		return nil, fmt.Errorf("task not found: %v", err)
	}

	var companyID uuid.UUID
	if err := s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID).Error; err != nil {
		return nil, fmt.Errorf("failed to get user company: %v", err)
	}
	if existingTask.CompanyID != companyID {
		return nil, fmt.Errorf("permission denied: task belongs to different company")
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

	tx := s.DB.WithContext(ctx).Begin()
	if err := tx.Model(&existingTask).Updates(task).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to update task: %v", err)
	}

	if len(task.AssigneeIDs) > 0 {
		if err := tx.Model(&existingTask).Association("Assignees").Clear(); err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to clear assignees: %v", err)
		}
		newAssignees := make([]models.Assignee, len(task.AssigneeIDs))
		for i, id := range task.AssigneeIDs {
			var assigneeCompanyID uuid.UUID
			if err := s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", id).Scan(&assigneeCompanyID).Error; err != nil || assigneeCompanyID != companyID {
				tx.Rollback()
				return nil, fmt.Errorf("assignee %s not found in company", id)
			}
			newAssignees[i] = models.Assignee{UserID: id}
		}
		if err := tx.Model(&existingTask).Association("Assignees").Append(newAssignees); err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to update assignees: %v", err)
		}
	}
	tx.Commit()

	if err := publishToRabbitMQ(s.RabbitMQChan, "tasks_queue", task); err != nil {
		log.Printf("Ошибка публикации в RabbitMQ: %v", err)
	}
	if s.GoogleCalendar.Client != nil {
		if eventID, err := s.Redis.Get(ctx, "calendar_event:"+task.ID.String()).Result(); err == nil {
			if err := s.GoogleCalendar.UpdateEvent(ctx, task.ID.String(), eventID, task.Title, task.Description, task.DueDate); err != nil {
				log.Printf("Ошибка обновления события в Google Calendar: %v", err)
			}
		}
	}

	s.invalidateCache(ctx, "tasks:*")
	publishTaskUpdate(s.Redis, task.ID.String(), "update", task.CreatedAt)
	return &existingTask, nil
}

func (s *TaskService) DeleteTask(ctx context.Context, taskID uuid.UUID, userID string) error {
	var task models.Task
	if err := s.DB.WithContext(ctx).First(&task, "id = ?", taskID).Error; err != nil {
		return err
	}
	s.logTaskChange(ctx, taskID, "deleted", task.Title, "")
	var role string
	if err := s.DB.WithContext(ctx).Raw("SELECT role FROM users WHERE user_id = ?", userID).Scan(&role).Error; err != nil {
		return fmt.Errorf("user not found: %v", err)
	}
	if role != "admin" && role != "super_admin" && task.CreatedBy != userID {
		return fmt.Errorf("permission denied")
	}

	deleteData := map[string]interface{}{
		"task_id": taskID.String(),
		"user_id": userID,
	}
	data, _ := json.Marshal(deleteData)
	if err := s.RabbitMQChan.Publish("", "tasks_queue", false, false, amqp.Publishing{
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
	if text == "" {
		return nil, fmt.Errorf("comment text is required")
	}

	var task models.Task
	if err := s.DB.WithContext(ctx).First(&task, "id = ?", taskID).Error; err != nil {
		return nil, fmt.Errorf("task not found: %v", err)
	}
	var companyID uuid.UUID
	if err := s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID).Error; err != nil {
		return nil, fmt.Errorf("failed to get user company: %v", err)
	}
	if task.CompanyID != companyID {
		return nil, fmt.Errorf("permission denied: task belongs to different company")
	}

	comment := &models.Comment{
		ID:        uuid.New(),
		TaskID:    taskID,
		UserID:    userID,
		Text:      text,
		CreatedAt: time.Now(),
	}
	if err := s.DB.WithContext(ctx).Create(comment).Error; err != nil {
		return nil, fmt.Errorf("failed to create comment: %v", err)
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
	var task models.Task
	if err := s.DB.WithContext(ctx).First(&task, taskID).Error; err != nil {
		return fmt.Errorf("task not found: %v", err)
	}
	var creatorCompanyID uuid.UUID
	if err := s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", task.CreatedBy).Scan(&creatorCompanyID).Error; err != nil {
		return fmt.Errorf("failed to get task creator company: %v", err)
	}
	var assigneeCompanyID uuid.UUID
	if err := s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&assigneeCompanyID).Error; err != nil {
		return fmt.Errorf("assignee not found: %v", err)
	}
	if creatorCompanyID != assigneeCompanyID {
		return fmt.Errorf("cannot assign task to user from different company")
	}
	assignee := models.Assignee{UserID: userID}
	if err := s.DB.WithContext(ctx).Model(&task).Association("Assignees").Append(&assignee); err != nil {
		return err
	}
	s.logTaskChange(ctx, taskID, "assignee", "", userID)
	s.invalidateCache(ctx, "tasks:*")
	return nil
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
		if assignees, ok := suggestionAction["assignee_ids"].([]interface{}); ok {
			task.AssigneeIDs = make([]string, len(assignees))
			for i, a := range assignees {
				task.AssigneeIDs[i] = a.(string)
			}
		} else if assignee, ok := suggestionAction["assignee"].(string); ok {
			task.AssigneeIDs = []string{assignee}
		}
		return s.CreateTask(ctx, userID, task, false)
	}

	if text == "" {
		text = "Новая задача"
	}

	task := &models.Task{ID: uuid.New(), Title: text, CreatedBy: userID, CreatedAt: time.Now(), Status: "pending"}
	return s.CreateTask(ctx, userID, task, true)
}

// GetTaskStats возвращает статистику задач для графиков
func (s *TaskService) GetTaskStats(ctx context.Context, userID, status, period string) (map[string]interface{}, error) {
	cacheKey := fmt.Sprintf("task_stats:%s:%s:%s", userID, status, period)
	if cached, err := s.Redis.Get(ctx, cacheKey).Bytes(); err == nil {
		var stats map[string]interface{}
		if err := json.Unmarshal(cached, &stats); err == nil {
			return stats, nil
		}
	}

	var companyID uuid.UUID
	if err := s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID).Error; err != nil {
		return nil, fmt.Errorf("user not found: %v", err)
	}

	result := map[string]interface{}{
		"completedLastWeek":       0,
		"completedYesterday":      0,
		"inProgressToday":         0,
		"totalCompletedLastWeek":  0,
		"totalCompletedYesterday": 0,
		"weeklyTrends":            map[string]int{},
	}

	weekStart := time.Now().AddDate(0, 0, -7)
	yesterdayStart := time.Now().AddDate(0, 0, -1).Truncate(24 * time.Hour)
	yesterdayEnd := yesterdayStart.Add(24*time.Hour - time.Second)
	todayStart := time.Now().Truncate(24 * time.Hour)
	todayEnd := todayStart.Add(24*time.Hour - time.Second)

	type TaskStats struct {
		CompletedLastWeek       int
		CompletedYesterday      int
		InProgressToday         int
		TotalCompletedLastWeek  float64
		TotalCompletedYesterday float64
	}

	var taskStats TaskStats
	query := `
        SELECT
            SUM(CASE WHEN status = 'completed' AND completed_at BETWEEN ? AND ? THEN 1 ELSE 0 END) as completed_last_week,
            SUM(CASE WHEN status = 'completed' AND completed_at BETWEEN ? AND ? THEN 1 ELSE 0 END) as completed_yesterday,
            SUM(CASE WHEN status = 'in_progress' AND updated_at BETWEEN ? AND ? THEN 1 ELSE 0 END) as in_progress_today,
            COALESCE(SUM(CASE WHEN status = 'completed' AND completed_at BETWEEN ? AND ? THEN cost ELSE 0 END), 0) as total_completed_last_week,
            COALESCE(SUM(CASE WHEN status = 'completed' AND completed_at BETWEEN ? AND ? THEN cost ELSE 0 END), 0) as total_completed_yesterday
        FROM tasks
        WHERE company_id = ?
    `
	if status != "" {
		query += " AND status = ?"
		if err := s.DB.WithContext(ctx).
			Raw(query, weekStart, time.Now(), yesterdayStart, yesterdayEnd, todayStart, todayEnd, weekStart, time.Now(), yesterdayStart, yesterdayEnd, companyID, status).
			Scan(&taskStats).Error; err != nil {
			return nil, err
		}
	} else {
		if err := s.DB.WithContext(ctx).
			Raw(query, weekStart, time.Now(), yesterdayStart, yesterdayEnd, todayStart, todayEnd, weekStart, time.Now(), yesterdayStart, yesterdayEnd, companyID).
			Scan(&taskStats).Error; err != nil {
			return nil, err
		}
	}

	result["completedLastWeek"] = taskStats.CompletedLastWeek
	result["completedYesterday"] = taskStats.CompletedYesterday
	result["inProgressToday"] = taskStats.InProgressToday
	result["totalCompletedLastWeek"] = int(taskStats.TotalCompletedLastWeek)
	result["totalCompletedYesterday"] = int(taskStats.TotalCompletedYesterday)

	weeklyTrends := make(map[string]int)
	for i := 0; i < 4; i++ {
		start := time.Now().AddDate(0, 0, -7*(i+1))
		end := start.AddDate(0, 0, 7)
		var count int64
		s.DB.WithContext(ctx).
			Model(&models.Task{}).
			Where("status = 'completed' AND company_id = ? AND completed_at BETWEEN ? AND ?", companyID, start, end).
			Count(&count)
		weekLabel := start.Format("2006-01-02")
		weeklyTrends[weekLabel] = int(count)
	}
	result["weeklyTrends"] = weeklyTrends

	if data, err := json.Marshal(result); err == nil {
		s.Redis.Set(ctx, cacheKey, data, 5*time.Minute)
	}

	return result, nil
}

func (s *TaskService) GetDashboard(ctx context.Context, userID string, filters map[string]string) (*Dashboard, error) {
	var companyID uuid.UUID
	if err := s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID).Error; err != nil {
		return nil, fmt.Errorf("user not found: %v", err)
	}
	var role string
	if err := s.DB.WithContext(ctx).Raw("SELECT role FROM users WHERE user_id = ?", userID).Scan(&role).Error; err != nil {
		return nil, fmt.Errorf("user not found: %v", err)
	}
	if role != "admin" && role != "manager" {
		return nil, fmt.Errorf("permission denied")
	}

	query := s.DB.WithContext(ctx).Where("company_id = ?", companyID)
	if date, ok := filters["date"]; ok {
		parsedDate, _ := time.Parse("2006-01-02", date)
		query = query.Where("due_date BETWEEN ? AND ?", parsedDate, parsedDate.Add(24*time.Hour))
	}
	if status, ok := filters["status"]; ok {
		query = query.Where("status = ?", status)
	}
	if userIDFilter, ok := filters["user_id"]; ok {
		query = query.Where("id IN (SELECT task_id FROM task_assignees WHERE user_id = ?)", userIDFilter)
	}

	var tasks []models.Task
	if err := query.Find(&tasks).Error; err != nil {
		return nil, err
	}

	employeeLoad := make(map[string]EmployeeLoad)
	completedTasks := 0
	overdueTasks := 0
	now := time.Now()
	for _, task := range tasks {
		if task.Status == "completed" {
			completedTasks++
		}
		if task.DueDate.Before(now) && task.Status != "completed" {
			overdueTasks++
		}
		for _, assignee := range task.Assignees {
			load := employeeLoad[assignee.UserID]
			load.UserID = assignee.UserID
			load.TaskCount++
			load.TotalPriority += task.Priority
			employeeLoad[assignee.UserID] = load
		}
	}

	// Преобразование значений карты в срез вручную
	employeeLoadSlice := make([]EmployeeLoad, 0, len(employeeLoad))
	for _, value := range employeeLoad {
		employeeLoadSlice = append(employeeLoadSlice, value)
	}

	return &Dashboard{
		EmployeeLoad:       employeeLoadSlice,
		TaskCount:          len(tasks),
		OverdueTasks:       overdueTasks,
		CompletedTasks:     completedTasks,
		NewClients:         0, // Здесь нужно добавить логику подсчёта новых клиентов
		TotalAmount:        0, // Добавить логику для суммы
		Productivity:       0, // Добавить расчёт продуктивности
		TasksChange:        0, // Добавить расчёт изменений
		ClientsChange:      0,
		AmountChange:       0,
		ProductivityChange: 0,
	}, nil
}

func (s *TaskService) GetMyTasks(ctx context.Context, userID string, limit, offset int, search string) ([]models.Task, int, error) {
	var companyID uuid.UUID
	if err := s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID).Error; err != nil {
		return nil, 0, fmt.Errorf("user not found: %v", err)
	}

	query := s.DB.WithContext(ctx).
		Preload("Assignees").
		Preload("Comments").
		Where("id IN (SELECT task_id FROM task_assignees WHERE user_id = ?) AND company_id = ?", userID, companyID)

	if search != "" {
		query = query.Where("title ILIKE ? OR description ILIKE ?", "%"+search+"%", "%"+search+"%")
	}

	var total int64
	if err := query.Model(&models.Task{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var tasks []models.Task
	if err := query.Limit(limit).Offset(offset).Find(&tasks).Error; err != nil {
		return nil, 0, err
	}

	return tasks, int(total), nil
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
		var task models.Task
		if err := tx.First(&task, taskID).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("task not found: %v", err)
		}
		var creatorCompanyID uuid.UUID
		if err := tx.Raw("SELECT company_id FROM users WHERE user_id = ?", task.CreatedBy).Scan(&creatorCompanyID).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to get creator company: %v", err)
		}
		var assigneeCompanyID uuid.UUID
		if err := tx.Raw("SELECT company_id FROM users WHERE user_id = ?", assigneeID).Scan(&assigneeCompanyID).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("assignee not found: %v", err)
		}
		if creatorCompanyID != assigneeCompanyID {
			tx.Rollback()
			return fmt.Errorf("cannot assign task to user from different company")
		}
		assignee := models.Assignee{UserID: assigneeID}
		if err := tx.Model(&task).Association("Assignees").Append(&assignee); err != nil {
			tx.Rollback()
			return err
		}
		s.logTaskChange(ctx, taskID, "assignee", "", assigneeID)
	}
	tx.Commit()
	s.invalidateCache(ctx, "tasks:*")
	return nil
}

// UpdateComment обновляет комментарий
func (s *TaskService) UpdateComment(ctx context.Context, taskID, commentID uuid.UUID, userID, text string) (*models.Comment, error) {
	var comment models.Comment
	if err := s.DB.WithContext(ctx).Where("id = ? AND task_id = ?", commentID, taskID).First(&comment).Error; err != nil {
		return nil, fmt.Errorf("comment not found")
	}

	if comment.UserID != userID {
		return nil, fmt.Errorf("permission denied")
	}

	oldText := comment.Text
	comment.Text = text
	if err := s.DB.WithContext(ctx).Save(&comment).Error; err != nil {
		return nil, err
	}

	s.logTaskChange(ctx, taskID, "comment_updated", oldText, text)
	s.invalidateCache(ctx, "tasks:*")
	event := struct {
		TaskID    string          `json:"task_id"`
		Action    string          `json:"action"`
		Comment   *models.Comment `json:"comment"`
		CreatedAt time.Time       `json:"created_at"`
	}{TaskID: taskID.String(), Action: "comment_updated", Comment: &comment, CreatedAt: time.Now()}
	eventJSON, _ := json.Marshal(event)
	s.Redis.Publish(ctx, "task_updates", eventJSON)

	return &comment, nil
}

// DeleteComment удаляет комментарий
func (s *TaskService) DeleteComment(ctx context.Context, taskID, commentID uuid.UUID, userID string) error {
	var comment models.Comment
	if err := s.DB.WithContext(ctx).Where("id = ? AND task_id = ?", commentID, taskID).First(&comment).Error; err != nil {
		return fmt.Errorf("comment not found")
	}

	if comment.UserID != userID {
		return fmt.Errorf("permission denied")
	}

	if err := s.DB.WithContext(ctx).Delete(&comment).Error; err != nil {
		return err
	}

	s.logTaskChange(ctx, taskID, "comment_deleted", comment.Text, "")
	s.invalidateCache(ctx, "tasks:*")
	event := struct {
		TaskID    string    `json:"task_id"`
		Action    string    `json:"action"`
		CommentID string    `json:"comment_id"`
		CreatedAt time.Time `json:"created_at"`
	}{TaskID: taskID.String(), Action: "comment_deleted", CommentID: commentID.String(), CreatedAt: time.Now()}
	eventJSON, _ := json.Marshal(event)
	s.Redis.Publish(ctx, "task_updates", eventJSON)

	return nil
}

// CreateFinancialEvent создает новое финансовое событие
func (s *TaskService) CreateFinancialEvent(ctx context.Context, userID string, event *FinancialEvent) (*FinancialEvent, error) {
	defer func() {
		if r := recover(); r != nil {
			stack := make([]byte, 1024)
			runtime.Stack(stack, false)
			log.Printf("Panic in CreateFinancialEvent: %v\nStack: %s", r, stack)
		}
	}()

	if event.Title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if event.DueDate.IsZero() {
		return nil, fmt.Errorf("due date is required")
	}
	if event.Amount < 0 {
		return nil, fmt.Errorf("amount cannot be negative")
	}

	event.EventID = uuid.New()
	event.UserID = userID
	event.CreatedAt = time.Now()
	if event.Status == "" {
		event.Status = "pending"
	}
	if event.Category == "" {
		event.Category = "other"
	}

	if err := s.DB.WithContext(ctx).Create(event).Error; err != nil {
		return nil, fmt.Errorf("failed to create financial event: %v", err)
	}

	if s.GoogleCalendar.Client != nil {
		eventID, err := s.GoogleCalendar.CreateEvent(ctx, event.EventID.String(), event.Title, "", event.DueDate)
		if err != nil {
			log.Printf("Failed to create Google Calendar event: %v", err)
		} else {
			s.Redis.Set(ctx, "calendar_event:financial:"+event.EventID.String(), eventID, 0)
		}
	}

	notification := map[string]interface{}{
		"user_id":  userID,
		"channel":  "email",
		"message":  fmt.Sprintf("Financial event '%s' scheduled for %s", event.Title, event.DueDate.Format("2006-01-02")),
		"priority": 5,
	}
	notificationJSON, _ := json.Marshal(notification)
	if err := s.RabbitMQChan.Publish("", "notifications", false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        notificationJSON,
	}); err != nil {
		log.Printf("Failed to publish notification: %v", err)
	}

	return event, nil
}

// GetFinancialEvents возвращает список финансовых событий пользователя
func (s *TaskService) GetFinancialEvents(ctx context.Context, userID string, startDate, endDate *time.Time) ([]FinancialEvent, error) {
	var companyID uuid.UUID
	if err := s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID).Error; err != nil {
		return nil, fmt.Errorf("user not found: %v", err)
	}

	var events []FinancialEvent
	query := s.DB.WithContext(ctx).Where("user_id = ?", userID)
	if startDate != nil && endDate != nil {
		query = query.Where("due_date BETWEEN ? AND ?", *startDate, *endDate)
	}
	if err := query.Find(&events).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch financial events: %v", err)
	}
	return events, nil
}

// StartTimeTracking начинает отслеживание времени для задачи
func (s *TaskService) StartTimeTracking(ctx context.Context, userID, taskID string) (*models.TimeEntry, error) {
	taskUUID, err := uuid.Parse(taskID)
	if err != nil {
		return nil, fmt.Errorf("invalid task ID: %v", err)
	}
	var activeEntry models.TimeEntry
	if err := s.DB.WithContext(ctx).Where("user_id = ? AND task_id = ? AND end_time IS NULL", userID, taskUUID).First(&activeEntry).Error; err == nil {
		return nil, fmt.Errorf("active time tracking already exists for task %s", taskID)
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
	s.invalidateCache(ctx, fmt.Sprintf("time_report:%s:*", userID))
	return entry, nil
}

// StopTimeTracking останавливает отслеживание времени
func (s *TaskService) StopTimeTracking(ctx context.Context, entryID string) (*models.TimeEntry, error) {
	entryUUID, err := uuid.Parse(entryID)
	if err != nil {
		return nil, fmt.Errorf("invalid entry ID: %v", err)
	}
	var entry models.TimeEntry
	if err := s.DB.WithContext(ctx).Where("id = ?", entryUUID).First(&entry).Error; err != nil {
		return nil, fmt.Errorf("time entry not found: %v", err)
	}
	if !entry.EndTime.IsZero() {
		return nil, fmt.Errorf("time tracking already stopped")
	}
	now := time.Now()
	entry.EndTime = now
	entry.Duration = int(now.Sub(entry.StartTime).Minutes())
	if err := s.DB.WithContext(ctx).Save(&entry).Error; err != nil {
		return nil, err
	}
	s.invalidateCache(ctx, fmt.Sprintf("time_report:%s:*", entry.UserID))
	return &entry, nil
}

// GetActiveTimeEntries возвращает активные записи времени для пользователя
func (s *TaskService) GetActiveTimeEntries(ctx context.Context, userID string) ([]models.TimeEntry, error) {
	var companyID uuid.UUID
	if err := s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID).Error; err != nil {
		return nil, fmt.Errorf("user not found: %v", err)
	}

	var entries []models.TimeEntry
	if err := s.DB.WithContext(ctx).
		Where("user_id = ? AND end_time IS NULL", userID).
		Joins("JOIN tasks ON tasks.id = time_entries.task_id").
		Where("tasks.company_id = ?", companyID).
		Find(&entries).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch active time entries: %v", err)
	}
	return entries, nil
}

// CalculateTotalActiveTime подсчитывает общее время активных записей в минутах
func (s *TaskService) CalculateTotalActiveTime(ctx context.Context, userID string) (int, error) {
	entries, err := s.GetActiveTimeEntries(ctx, userID)
	if err != nil {
		return 0, err
	}
	totalMinutes := 0
	now := time.Now()
	for _, entry := range entries {
		totalMinutes += int(now.Sub(entry.StartTime).Minutes())
	}
	return totalMinutes, nil
}

func (s *TaskService) CreateOnboardingTasks(ctx context.Context, userID, companyType string) error {
	var companyID uuid.UUID
	if err := s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID).Error; err != nil {
		return fmt.Errorf("user not found: %v", err)
	}

	onboardingTasks := []struct {
		Title       string
		Description string
		Priority    string
	}{
		{"Заполнить профиль", "Укажите дополнительные данные в профиле", "high"},
		{"Подключить Google Calendar", "Настройте интеграцию с календарем", "medium"},
	}
	if companyType == "ooo" {
		onboardingTasks = append(onboardingTasks, struct {
			Title       string
			Description string
			Priority    string
		}{"Добавить сотрудников", "Пригласите коллег в систему", "medium"})
	}

	dueDate := time.Now().Add(7 * 24 * time.Hour)
	for _, task := range onboardingTasks {
		priorityMap := map[string]int{"high": 3, "medium": 2, "low": 1}
		newTask := &models.Task{
			ID:          uuid.New(),
			Title:       task.Title,
			Description: task.Description,
			Priority:    priorityMap[task.Priority],
			CreatedBy:   userID,
			CreatedAt:   time.Now(),
			DueDate:     dueDate,
			Status:      "pending",
		}
		if err := s.DB.WithContext(ctx).Create(newTask).Error; err != nil {
			log.Printf("Ошибка создания онбординг-задачи: %v", err)
			return err
		}

		// Интеграция с Google Calendar, если настроена
		if s.GoogleCalendar.Client != nil {
			eventID, err := s.GoogleCalendar.CreateEvent(ctx, newTask.ID.String(), newTask.Title, newTask.Description, newTask.DueDate)
			if err != nil {
				log.Printf("Ошибка создания события в Google Calendar: %v", err)
			} else {
				s.Redis.Set(ctx, "calendar_event:"+newTask.ID.String(), eventID, 0)
			}
		}

		// Уведомление через RabbitMQ
		notification := map[string]interface{}{
			"type": "notify_user",
			"data": map[string]interface{}{
				"user_id":  userID,
				"message":  fmt.Sprintf("Новая онбординг-задача: %s, дедлайн: %s", newTask.Title, newTask.DueDate.Format("2006-01-02")),
				"priority": 5,
			},
		}
		notificationJSON, _ := json.Marshal(notification)
		if err := s.RabbitMQChan.Publish("", "notifications_queue", false, false, amqp.Publishing{
			ContentType: "application/json",
			Body:        notificationJSON,
		}); err != nil {
			log.Printf("Ошибка отправки уведомления: %v", err)
		}

		s.invalidateCache(ctx, "tasks:*")
		event := struct {
			TaskID    string    `json:"task_id"`
			Action    string    `json:"action"`
			CreatedAt time.Time `json:"created_at"`
		}{TaskID: newTask.ID.String(), Action: "create", CreatedAt: newTask.CreatedAt}
		eventJSON, _ := json.Marshal(event)
		s.Redis.Publish(ctx, "task_updates", eventJSON)
	}
	log.Printf("Онбординг-задачи созданы для пользователя %s", userID)
	return nil
}

// invalidateCache инвалидирует кэш по заданному шаблону
func (s *TaskService) invalidateCache(ctx context.Context, pattern string) {
	keys, err := s.Redis.Keys(ctx, pattern).Result()
	if err != nil {
		log.Printf("Ошибка получения ключей из Redis: %v", err)
		return
	}
	for _, key := range keys {
		s.Redis.Del(ctx, key)
		log.Printf("Кэш инвалидирован для %s", key)
	}
}

// GetTimeReport возвращает отчет по времени с кэшированием
func (s *TaskService) GetTimeReport(ctx context.Context, userID string, period string) ([]models.TimeEntry, error) {
	cacheKey := fmt.Sprintf("time_report:%s:%s", userID, period)
	if cached, err := s.Redis.Get(ctx, cacheKey).Bytes(); err == nil {
		var entries []models.TimeEntry
		if err := json.Unmarshal(cached, &entries); err == nil {
			log.Printf("GetTimeReport: данные получены из кэша для %s", cacheKey)
			return entries, nil
		}
	}

	var companyID uuid.UUID
	if err := s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID).Error; err != nil {
		return nil, fmt.Errorf("user not found: %v", err)
	}

	var entries []models.TimeEntry
	query := s.DB.WithContext(ctx).
		Where("user_id = ?", userID).
		Joins("JOIN tasks ON tasks.id = time_entries.task_id").
		Where("tasks.company_id = ?", companyID)
	switch period {
	case "day":
		query = query.Where("start_time >= ?", time.Now().Truncate(24*time.Hour))
	case "week":
		query = query.Where("start_time >= ?", time.Now().AddDate(0, 0, -7))
	case "month":
		query = query.Where("start_time >= ?", time.Now().AddDate(0, -1, 0))
	}
	if err := query.Find(&entries).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch time report: %v", err)
	}

	if data, err := json.Marshal(entries); err == nil {
		s.Redis.Set(ctx, cacheKey, data, 10*time.Minute)
		log.Printf("GetTimeReport: данные сохранены в кэш для %s", cacheKey)
	}

	return entries, nil
}

func (s *TaskService) CreateOnboardingChecklist(ctx context.Context, userID string) error {
	checklist := []models.OnboardingChecklist{
		{ID: uuid.New(), UserID: userID, TaskTitle: "Заполнить профиль", DueDate: time.Now().Add(24 * time.Hour)},
		{ID: uuid.New(), UserID: userID, TaskTitle: "Пройти обучение", DueDate: time.Now().Add(48 * time.Hour)},
	}
	if err := s.DB.Create(&checklist).Error; err != nil {
		return err
	}

	// Отправка уведомления через RabbitMQ
	for _, item := range checklist {
		notification := map[string]interface{}{
			"type": "notify_user",
			"data": map[string]interface{}{
				"user_id":  userID,
				"message":  fmt.Sprintf("Завершите задачу: %s к %s", item.TaskTitle, item.DueDate),
				"priority": 5,
			},
		}
		notificationJSON, _ := json.Marshal(notification)
		s.RabbitMQChan.Publish("", "notifications_queue", false, false, amqp.Publishing{
			ContentType: "application/json",
			Body:        notificationJSON,
		})
	}
	return nil
}

// CalculatePayroll рассчитывает зарплату с кэшированием
func (s *TaskService) CalculatePayroll(ctx context.Context, userID, period string) (*models.Payroll, error) {
	cacheKey := fmt.Sprintf("payroll:%s:%s", userID, period)
	if cached, err := s.Redis.Get(ctx, cacheKey).Bytes(); err == nil {
		var payroll models.Payroll
		if err := json.Unmarshal(cached, &payroll); err == nil {
			log.Printf("CalculatePayroll: данные получены из кэша для %s", cacheKey)
			return &payroll, nil
		}
	}

	var companyID uuid.UUID
	if err := s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID).Error; err != nil {
		return nil, fmt.Errorf("user not found: %v", err)
	}

	var totalHours int
	type User struct {
		Rate    float64 `gorm:"column:rate"`
		TaxRate float64 `gorm:"column:tax_rate"`
	}
	var user User
	if err := s.DB.WithContext(ctx).Table("users").Where("user_id = ? AND company_id = ?", userID, companyID).First(&user).Error; err != nil {
		return nil, fmt.Errorf("user not found in company: %v", err)
	}

	entries := []models.TimeEntry{}
	if err := s.DB.WithContext(ctx).
		Where("user_id = ? AND start_time LIKE ?", userID, period+"%").
		Joins("JOIN tasks ON tasks.id = time_entries.task_id").
		Where("tasks.company_id = ?", companyID).
		Find(&entries).Error; err != nil {
		return nil, err
	}
	for _, entry := range entries {
		totalHours += entry.Duration / 60
	}

	grossSalary := float64(totalHours) * user.Rate
	tax := grossSalary * user.TaxRate
	netSalary := grossSalary - tax

	payroll := &models.Payroll{
		ID:         uuid.New(),
		UserID:     userID,
		Period:     period,
		Hours:      totalHours,
		Rate:       user.Rate,
		TaxRate:    user.TaxRate,
		NetSalary:  netSalary,
		Calculated: time.Now(),
	}
	if err := s.DB.WithContext(ctx).Create(payroll).Error; err != nil {
		return nil, err
	}

	if data, err := json.Marshal(payroll); err == nil {
		s.Redis.Set(ctx, cacheKey, data, 1*time.Hour)
		log.Printf("CalculatePayroll: данные сохранены в кэш для %s", cacheKey)
	}

	s.invalidateCache(ctx, fmt.Sprintf("time_report:%s:*", userID))
	return payroll, nil
}

// GetAggregatePayroll возвращает агрегированные данные о зарплате всех сотрудников за указанный период
func (s *TaskService) GetAggregatePayroll(ctx context.Context, userID, period string) ([]map[string]interface{}, error) {
	// Проверка роли пользователя
	var role string
	if err := s.DB.WithContext(ctx).Raw("SELECT role FROM users WHERE user_id = ?", userID).Scan(&role).Error; err != nil {
		return nil, fmt.Errorf("user not found: %v", err)
	}
	if role != "admin" {
		return nil, fmt.Errorf("only admins can access aggregate payroll data")
	}

	// Получаем company_id пользователя
	var companyID uuid.UUID
	if err := s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID).Error; err != nil {
		return nil, fmt.Errorf("company not found: %v", err)
	}

	// Кэширование результата
	cacheKey := fmt.Sprintf("aggregate_payroll:%s:%s", companyID.String(), period)
	if cached, err := s.Redis.Get(ctx, cacheKey).Bytes(); err == nil {
		var payrollData []map[string]interface{}
		if err := json.Unmarshal(cached, &payrollData); err == nil {
			log.Printf("GetAggregatePayroll: данные получены из кэша для %s", cacheKey)
			return payrollData, nil
		}
	}

	// Запрос к базе данных для получения агрегированных данных
	var payrollData []map[string]interface{}
	query := `
        SELECT 
            u.user_id AS employee_id,
            u.name AS employee_name,
            u.department AS department,
            p.net_salary AS amount,
            p.period AS period
        FROM payroll p
        JOIN users u ON p.user_id = u.user_id
        WHERE u.company_id = ? AND p.period = ?
    `
	if err := s.DB.WithContext(ctx).
		Raw(query, companyID, period).
		Scan(&payrollData).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch aggregate payroll data: %v", err)
	}

	// Сохраняем в кэш с TTL 1 час
	if data, err := json.Marshal(payrollData); err == nil {
		s.Redis.Set(ctx, cacheKey, data, 1*time.Hour)
		log.Printf("GetAggregatePayroll: данные сохранены в кэш для %s", cacheKey)
	}

	return payrollData, nil
}

// Новая функция в task_service.go
func (s *TaskService) GetTaskComments(ctx context.Context, taskID uuid.UUID) ([]models.Comment, error) {
	var task models.Task
	if err := s.DB.WithContext(ctx).First(&task, "id = ?", taskID).Error; err != nil {
		return nil, fmt.Errorf("task not found: %v", err)
	}
	// Проверка доступа (например, пользователь должен быть создателем или исполнителем)
	userID := ctx.Value("user_id").(string)
	var isAssignee int64
	s.DB.WithContext(ctx).Model(&models.Assignee{}).Where("task_id = ? AND user_id = ?", taskID, userID).Count(&isAssignee)
	if task.CreatedBy != userID && isAssignee == 0 {
		return nil, fmt.Errorf("permission denied")
	}
	var comments []models.Comment
	if err := s.DB.WithContext(ctx).Where("task_id = ?", taskID).Find(&comments).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch comments: %v", err)
	}
	return comments, nil
}

func (s *TaskService) CreateLeaveRequest(ctx context.Context, userID, leaveType string, startDate, endDate time.Time) (*models.LeaveRequest, error) {
	var companyID uuid.UUID
	if err := s.DB.WithContext(ctx).Raw("SELECT company_id FROM users WHERE user_id = ?", userID).Scan(&companyID).Error; err != nil {
		return nil, fmt.Errorf("failed to get user company: %v", err)
	}

	var overlapping int64
	s.DB.WithContext(ctx).Model(&models.LeaveRequest{}).
		Where("user_id = ? AND company_id = ? AND (start_date <= ? AND end_date >= ?)", userID, companyID, endDate, startDate).
		Count(&overlapping)
	if overlapping > 0 {
		return nil, fmt.Errorf("leave request overlaps with existing request")
	}

	request := &models.LeaveRequest{
		ID:        uuid.New(),
		UserID:    userID,
		Type:      leaveType,
		StartDate: startDate,
		EndDate:   endDate,
		Status:    "pending",
		CompanyID: companyID,
	}
	if err := s.DB.Create(request).Error; err != nil {
		return nil, err
	}

	if s.GoogleCalendar.Client != nil {
		eventID, err := s.GoogleCalendar.CreateEvent(ctx, request.ID.String(), fmt.Sprintf("%s: %s", leaveType, userID), "", startDate)
		if err != nil {
			log.Printf("Ошибка создания события в Google Calendar: %v", err)
		} else {
			s.Redis.Set(ctx, "calendar_event:leave:"+request.ID.String(), eventID, 0)
		}
	}

	notification := map[string]interface{}{
		"type": "notify_user",
		"data": map[string]interface{}{
			"user_id":  userID,
			"message":  fmt.Sprintf("Запрос на %s с %s по %s создан", leaveType, startDate.Format("2006-01-02"), endDate.Format("2006-01-02")),
			"priority": 5,
		},
	}
	notificationJSON, _ := json.Marshal(notification)
	if err := s.RabbitMQChan.Publish("", "notifications_queue", false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        notificationJSON,
	}); err != nil {
		log.Printf("Ошибка отправки уведомления: %v", err)
	}

	return request, nil
}

func (s *TaskService) CreateFollowUpTask(ctx context.Context, userID string, originalTaskID uuid.UUID) (*models.Task, error) {
	var originalTask models.Task
	if err := s.DB.WithContext(ctx).First(&originalTask, "id = ?", originalTaskID).Error; err != nil {
		return nil, fmt.Errorf("original task not found: %v", err)
	}
	followUpTask := &models.Task{
		ID:          uuid.New(),
		Title:       "Follow-up: " + originalTask.Title,
		Description: "Следующая задача после " + originalTask.Title,
		CreatedBy:   userID,
		CreatedAt:   time.Now(),
		DueDate:     time.Now().Add(24 * time.Hour),
		Status:      "pending",
		Priority:    originalTask.Priority,
		CompanyID:   originalTask.CompanyID,
	}
	return s.CreateTask(ctx, userID, followUpTask, false)
}

// ConsumeRabbitMQ обрабатывает сообщения из очереди
func (s *TaskService) ConsumeRabbitMQ(ctx context.Context) {
	queue, err := s.RabbitMQChan.QueueDeclare("task_queue", true, false, false, false, nil)
	if err != nil {
		log.Fatalf("Ошибка объявления очереди task_queue: %v", err)
	}
	recurringQueue, err := s.RabbitMQChan.QueueDeclare("recurring_tasks", true, false, false, false, nil)
	if err != nil {
		log.Fatalf("Ошибка объявления очереди recurring_tasks: %v", err)
	}

	consumeWithRetry := func(queueName string, messages <-chan amqp.Delivery, handler func(amqp.Delivery)) {
		for msg := range messages {
			retries := 3
			for i := 0; i < retries; i++ {
				err := func() error {
					defer func() {
						if r := recover(); r != nil {
							stack := make([]byte, 1024)
							runtime.Stack(stack, false)
							log.Printf("Паника при обработке сообщения из %s: %v\nСтек: %s", queueName, r, stack)
						}
					}()
					handler(msg)
					return nil
				}()
				if err == nil {
					break
				}
				log.Printf("Ошибка обработки сообщения из %s (попытка %d/%d): %v", queueName, i+1, retries, err)
				time.Sleep(time.Second * time.Duration(i+1))
			}
		}
	}

	taskMsgs, err := s.RabbitMQChan.Consume(queue.Name, "", false, false, false, false, nil)
	if err != nil {
		log.Fatalf("Ошибка потребления из очереди task_queue: %v", err)
	}
	go consumeWithRetry(queue.Name, taskMsgs, func(msg amqp.Delivery) {
		var action map[string]interface{}
		if err := json.Unmarshal(msg.Body, &action); err != nil {
			log.Printf("Ошибка разбора сообщения: %v", err)
			msg.Nack(false, true)
			return
		}
		actionType, ok := action["type"].(string)
		if !ok {
			log.Printf("Ошибка: тип действия не указан")
			msg.Nack(false, true)
			return
		}
		data, ok := action["data"].(map[string]interface{})
		if !ok {
			log.Printf("Ошибка: данные действия не валидны")
			msg.Nack(false, true)
			return
		}

		switch actionType {
		case "start_time_tracking":
			userID, _ := data["user_id"].(string)
			taskID, _ := data["task_id"].(string)
			if _, err := s.StartTimeTracking(ctx, userID, taskID); err != nil {
				log.Printf("Ошибка старта трекинга: %v", err)
				msg.Nack(false, true)
				return
			}
		default:
			log.Printf("Неизвестный тип действия: %s", actionType)
			msg.Nack(false, true)
			return
		}
		msg.Ack(false)
	})

	recurringMsgs, err := s.RabbitMQChan.Consume(recurringQueue.Name, "", false, false, false, false, nil)
	if err != nil {
		log.Fatalf("Ошибка потребления из очереди recurring_tasks: %v", err)
	}
	go consumeWithRetry(recurringQueue.Name, recurringMsgs, func(msg amqp.Delivery) {
		var job map[string]interface{}
		if err := json.Unmarshal(msg.Body, &job); err != nil {
			log.Printf("Ошибка разбора сообщения recurring_tasks: %v", err)
			msg.Nack(false, true)
			return
		}
		if jobType, ok := job["type"].(string); ok && jobType == "create_recurring_task" {
			interval, _ := job["interval"].(string)
			untilStr, _ := job["until"].(string)
			until, err := time.Parse(time.RFC3339, untilStr)
			if err != nil {
				log.Printf("Ошибка парсинга until: %v", err)
				msg.Nack(false, true)
				return
			}
			nextDue := time.Now()
			switch interval {
			case "daily":
				nextDue = nextDue.Add(24 * time.Hour)
			case "weekly":
				nextDue = nextDue.Add(7 * 24 * time.Hour)
			case "monthly":
				nextDue = nextDue.AddDate(0, 1, 0)
			default:
				log.Printf("Неподдерживаемый интервал: %s", interval)
				msg.Nack(false, true)
				return
			}
			if nextDue.Before(until) {
				newTask := &models.Task{
					ID:          uuid.New(),
					Title:       job["title"].(string),
					Description: job["description"].(string),
					DueDate:     nextDue,
					CreatedBy:   job["user_id"].(string),
					CreatedAt:   time.Now(),
					Status:      "pending",
					Priority:    int(job["priority"].(float64)),
				}
				if _, err := s.CreateTask(ctx, job["user_id"].(string), newTask, true); err != nil {
					log.Printf("Ошибка создания повторяющейся задачи: %v", err)
					msg.Nack(false, true)
					return
				}
				if err := s.RabbitMQChan.Publish("", "recurring_tasks", false, false, amqp.Publishing{
					ContentType: "application/json",
					Body:        msg.Body,
				}); err != nil {
					log.Printf("Ошибка повторной отправки: %v", err)
				}
			}
		}
		msg.Ack(false)
	})
}
