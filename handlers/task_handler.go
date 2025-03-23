package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/andre/task-service/config"
	"github.com/andre/task-service/models"
	"github.com/andre/task-service/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type TaskHandler struct {
	Service *services.TaskService
	Config  config.Config
}

func NewTaskHandler(service *services.TaskService, cfg config.Config) *TaskHandler {
	return &TaskHandler{Service: service, Config: cfg}
}

func (h *TaskHandler) getErrorMessage(key string) string {
	messages := map[string]map[string]string{
		"invalid_input":     {"en": "Invalid input", "ru": "Неверный ввод"},
		"permission_denied": {"en": "Permission denied", "ru": "Доступ запрещен"},
		"task_not_found":    {"en": "Task not found", "ru": "Задача не найдена"},
		"server_error":      {"en": "Server error", "ru": "Ошибка сервера"},
		"user_not_found":    {"en": "User not found", "ru": "Пользователь не найден"},
	}
	lang := h.Config.Language
	if msg, ok := messages[key][lang]; ok {
		return msg
	}
	return messages[key]["en"] // По умолчанию английский
}

// CreateTask godoc
// @Summary Create a new task
// @Description Create a task with optional assignees
// @Tags tasks
// @Accept json
// @Produce json
// @Param task body object true "Task data" {"title": "string", "description": "string", "status": "string", "due_date": "string", "priority": "int", "assignee_ids": ["string"]}
// @Success 201 {object} models.Task
// @Failure 400 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/create [post]
func (h *TaskHandler) CreateTask(c *gin.Context) {
	var taskInput struct {
		models.Task
		AssigneeIDs []string `json:"assignee_ids"`
	}
	if err := c.ShouldBindJSON(&taskInput); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input") + ": " + err.Error()})
		return
	}

	// Добавляем валидацию поля Title
	if taskInput.Title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input") + ": title is required"})
		return
	}

	userID := c.GetString("user_id")
	taskInput.ID = uuid.New()
	taskInput.CreatedAt = time.Now()
	taskInput.Status = "pending"

	task, err := h.Service.CreateTask(c.Request.Context(), userID, &taskInput.Task, len(taskInput.AssigneeIDs) == 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error")})
		return
	}
	for _, assigneeID := range taskInput.AssigneeIDs {
		if err := h.Service.AddAssignee(c.Request.Context(), task.ID, assigneeID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error")})
			return
		}
	}
	c.JSON(http.StatusCreated, task)
}

// GetTaskStats godoc
// @Summary Get task statistics for dashboard charts
// @Description Retrieve task statistics for a specific user with filters
// @Tags tasks
// @Produce json
// @Param status query string false "Task status (pending, in_progress, completed)"
// @Param period query string false "Period (week, yesterday, today, month, year)"
// @Success 200 {object} map[string]int "statistics response"
// @Failure 400 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /task [get]
func (h *TaskHandler) GetTaskStats(c *gin.Context) {
	userID := c.GetString("user_id")
	status := c.Query("status")
	period := c.Query("period")

	// Валидация параметров
	if period != "" && period != "week" && period != "yesterday" && period != "today" && period != "month" && period != "year" {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input") + ": invalid period"})
		return
	}
	if status != "" && status != "pending" && status != "in_progress" && status != "completed" {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input") + ": invalid status"})
		return
	}

	stats, err := h.Service.GetTaskStats(c.Request.Context(), userID, status, period)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error")})
		return
	}
	c.JSON(http.StatusOK, stats)
}

// GetTask godoc
// @Summary Get a task by ID
// @Description Retrieve a specific task with assignees and comments
// @Tags tasks
// @Produce json
// @Param id path string true "Task ID"
// @Success 200 {object} models.Task
// @Failure 400 {object} map[string]string "error response"
// @Failure 404 {object} map[string]string "error response"
// @Router /tasks/{id} [get]
func (h *TaskHandler) GetTask(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input")})
		return
	}

	task, err := h.Service.GetTask(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": h.getErrorMessage("task_not_found")})
		return
	}

	c.JSON(http.StatusOK, task)
}

// GetTasks godoc
// @Summary Get list of tasks
// @Description Retrieve tasks with optional filters
// @Tags tasks
// @Produce json
// @Param status query string false "Task status"
// @Param due_date query string false "Due date (YYYY-MM-DD)"
// @Param limit query int false "Limit" default(10)
// @Param offset query int false "Offset" default(0)
// @Success 200 {object} object "response" {"tasks": "array", "total": "int"}
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks [get]
func (h *TaskHandler) GetTasks(c *gin.Context) {
	status := c.Query("status")
	dueDateStr := c.Query("due_date")
	priority := c.Query("priority")
	assigneeID := c.Query("assignee_id")
	fields := c.Query("fields") // Новый параметр
	limit, _ := c.GetQuery("limit")
	offset, _ := c.GetQuery("offset")

	var dueDate *time.Time
	if dueDateStr != "" {
		parsedDate, err := time.Parse("2006-01-02", dueDateStr)
		if err == nil {
			dueDate = &parsedDate
		}
	}

	lim, _ := strconv.Atoi(limit)
	if lim == 0 {
		lim = 10
	}
	off, _ := strconv.Atoi(offset)

	tasks, total, err := h.Service.GetTasks(c.Request.Context(), status, dueDate, priority, assigneeID, fields, lim, off)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error")})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tasks": tasks, "total": total})
}

// UpdateTask godoc
// @Summary Update a task
// @Description Update task details and assignees
// @Tags tasks
// @Accept json
// @Produce json
// @Param id path string true "Task ID"
// @Param task body object true "Task data" {"title": "string", "description": "string", "status": "string", "due_date": "string", "priority": "int", "assignee_ids": ["string"]}
// @Success 200 {object} models.Task
// @Failure 400 {object} map[string]string "error response"
// @Failure 403 {object} map[string]string "error response"
// @Failure 404 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/{id} [put]
func (h *TaskHandler) UpdateTask(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input")})
		return
	}

	var taskInput struct {
		Title       string    `json:"title" binding:"max=255"`
		Description string    `json:"description" binding:"max=1000"`
		Status      string    `json:"status" binding:"oneof=pending in_progress completed"`
		DueDate     time.Time `json:"due_date"`
		Priority    int       `json:"priority"`
		AssigneeIDs []string  `json:"assignee_ids"`
	}
	if err := c.ShouldBindJSON(&taskInput); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input") + ": " + err.Error()})
		return
	}

	userID := c.GetString("user_id")
	role := c.GetString("role")
	// Исправлено: GetTask возвращает *models.Task и error
	task, err := h.Service.GetTask(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": h.getErrorMessage("task_not_found")})
		return
	}

	// Убрано шардирование, проверка прав доступа упрощена
	if role != "super_admin" && ((role == "admin" || role == "manager") && task.CreatedBy != userID) {
		c.JSON(http.StatusForbidden, gin.H{"error": h.getErrorMessage("permission_denied")})
		return
	}

	updatedTask := models.Task{
		ID:          id,
		Title:       taskInput.Title,
		Description: taskInput.Description,
		Status:      taskInput.Status,
		DueDate:     taskInput.DueDate,
		Priority:    taskInput.Priority,
	}
	task, err = h.Service.UpdateTask(c.Request.Context(), userID, &updatedTask)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error")})
		return
	}

	for _, assigneeID := range taskInput.AssigneeIDs {
		if err := h.Service.AddAssignee(c.Request.Context(), id, assigneeID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error")})
			return
		}
	}
	c.JSON(http.StatusOK, task)
}

// DeleteTask godoc
// @Summary Delete a task
// @Description Delete a task by ID
// @Tags tasks
// @Param id path string true "Task ID"
// @Success 200 {object} map[string]string "message response"
// @Failure 400 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/{id} [delete]
func (h *TaskHandler) DeleteTask(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input")})
		return
	}

	userID := c.GetString("user_id")
	role := c.GetString("role")
	task, err := h.Service.GetTask(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": h.getErrorMessage("task_not_found")})
		return
	}
	if role != "admin" && role != "super_admin" && task.CreatedBy != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": h.getErrorMessage("permission_denied")})
		return
	}

	if err := h.Service.DeleteTask(c.Request.Context(), id, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error")})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Task deleted"})
}

// AddComment godoc
// @Summary Add a comment to a task
// @Description Add a comment to a specific task
// @Tags tasks
// @Accept json
// @Produce json
// @Param id path string true "Task ID"
// @Param comment body object true "Comment text" {"text": "string"}
// @Success 201 {object} models.Comment
// @Failure 400 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/{id}/comments [post]
func (h *TaskHandler) AddComment(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input")})
		return
	}

	var input struct {
		Text string `json:"text"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input")})
		return
	}

	userID := c.GetString("user_id")
	comment, err := h.Service.AddComment(c.Request.Context(), id, userID, input.Text)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error")})
		return
	}
	c.JSON(http.StatusCreated, comment)
}

// GenerateReport godoc
// @Summary Generate a report
// @Description Generate a report for a specified period
// @Tags tasks
// @Produce json
// @Param period query string true "Period (day, week, month)"
// @Success 200 {object} services.Report
// @Failure 403 {object} map[string]string "error response"
// @Router /tasks/report [post]
func (h *TaskHandler) GenerateReport(c *gin.Context) {
	userID := c.GetString("user_id")
	period := c.Query("period")
	report, err := h.Service.GenerateReport(c.Request.Context(), userID, period)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": h.getErrorMessage("permission_denied")})
		return
	}
	c.JSON(http.StatusOK, report)
}

// GetReports godoc
// @Summary Get list of reports
// @Description Retrieve all reports for the user's company
// @Tags tasks
// @Produce json
// @Success 200 {array} services.Report
// @Failure 403 {object} map[string]string "error response"
// @Router /tasks/reports [get]
func (h *TaskHandler) GetReports(c *gin.Context) {
	userID := c.GetString("user_id")
	reports, err := h.Service.GetReports(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": h.getErrorMessage("permission_denied")})
		return
	}
	c.JSON(http.StatusOK, reports)
}

// CreateTaskWithPrediction godoc
// @Summary Create a task with predictive input
// @Description Create a task based on text with predictive suggestions
// @Tags tasks
// @Accept json
// @Produce json
// @Param request body object true "Task creation request" {"text": "string", "confirm_suggestion": "bool", "suggestion_action": "object"}
// @Success 201 {object} models.Task
// @Failure 400 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/create/predictive [post]
func (h *TaskHandler) CreateTaskWithPrediction(c *gin.Context) {
	userID := c.GetString("user_id")
	var req struct {
		Text              string                 `json:"text"`
		ConfirmSuggestion bool                   `json:"confirm_suggestion"`
		SuggestionAction  map[string]interface{} `json:"suggestion_action"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input") + ": " + err.Error()})
		return
	}

	task, err := h.Service.CreateTaskWithPrediction(c.Request.Context(), userID, req.Text, req.ConfirmSuggestion, req.SuggestionAction)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error")})
		return
	}
	c.JSON(http.StatusCreated, task)
}

// GetDashboard godoc
// @Summary Get admin dashboard
// @Description Retrieve dashboard data with filters
// @Tags tasks
// @Produce json
// @Param date query string false "Filter by date (YYYY-MM-DD)"
// @Param status query string false "Filter by status"
// @Param user_id query string false "Filter by user ID"
// @Success 200 {object} services.Dashboard
// @Failure 403 {object} map[string]string "error response"
// @Failure 404 {object} map[string]string "error response"
// @Router /tasks/dashboard [get]
func (h *TaskHandler) GetDashboard(c *gin.Context) {
	userID := c.GetString("user_id")
	filters := map[string]string{
		"date":    c.Query("date"),
		"status":  c.Query("status"),
		"user_id": c.Query("user_id"),
	}
	dashboard, err := h.Service.GetDashboard(c.Request.Context(), userID, filters)
	if err != nil {
		if err.Error() == "user not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": h.getErrorMessage("user_not_found")})
		} else {
			c.JSON(http.StatusForbidden, gin.H{"error": h.getErrorMessage("permission_denied")})
		}
		return
	}
	c.JSON(http.StatusOK, dashboard)
}

// GetMyTasks godoc
// @Summary Get user's tasks
// @Description Retrieve tasks assigned to the current user
// @Tags tasks
// @Produce json
// @Success 200 {object} object "response" {"tasks": "array"}
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/my [get]
func (h *TaskHandler) GetMyTasks(c *gin.Context) {
	userID := c.GetString("user_id")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	search := c.Query("search")

	tasks, total, err := h.Service.GetMyTasks(c.Request.Context(), userID, limit, offset, search)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tasks": tasks, "total": total})
}

// GetTaskSuggestions godoc
// @Summary Get task suggestions
// @Description Retrieve contextual suggestions based on text input
// @Tags tasks
// @Produce json
// @Param text query string true "Input text for suggestions"
// @Success 200 {object} object "response" {"suggestions": "array"}
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/suggestions [get]
func (h *TaskHandler) GetTaskSuggestions(c *gin.Context) {
	userID := c.GetString("user_id")
	text := c.Query("text")
	suggestions, err := h.Service.GetTaskSuggestions(c.Request.Context(), userID, text)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error")})
		return
	}
	c.JSON(http.StatusOK, gin.H{"suggestions": suggestions})
}

// GenerateQuickReport godoc
// @Summary Generate a quick report
// @Description Generate a report for a specified period with default week
// @Tags tasks
// @Produce json
// @Param period query string false "Period (day, week, month)" default(week)
// @Success 200 {object} services.Report
// @Failure 403 {object} map[string]string "error response"
// @Router /tasks/report/quick [post]
func (h *TaskHandler) GenerateQuickReport(c *gin.Context) {
	userID := c.GetString("user_id")
	period := c.Query("period")
	if period == "" {
		period = "week" // По умолчанию за неделю
	}
	report, err := h.Service.GenerateReport(c.Request.Context(), userID, period)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": h.getErrorMessage("permission_denied")})
		return
	}
	c.JSON(http.StatusOK, report)
}

// BulkAssignTasks godoc
// @Summary Bulk assign tasks
// @Description Assign multiple tasks to a single user
// @Tags tasks
// @Accept json
// @Produce json
// @Param request body object true "Task assignment request" {"task_ids": ["string"], "assignee_id": "string"}
// @Success 200 {object} map[string]string "message response"
// @Failure 400 {object} map[string]string "error response"
// @Failure 403 {object} map[string]string "error response"
// @Router /tasks/bulk_assign [post]
func (h *TaskHandler) BulkAssignTasks(c *gin.Context) {
	userID := c.GetString("user_id")
	var req struct {
		TaskIDs    []uuid.UUID `json:"task_ids"`
		AssigneeID string      `json:"assignee_id"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input") + ": " + err.Error()})
		return
	}
	if err := h.Service.BulkAssignTasks(c.Request.Context(), userID, req.TaskIDs, req.AssigneeID); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": h.getErrorMessage("permission_denied")})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Tasks assigned"})
}

// UpdateComment godoc
// @Summary Update a comment
// @Description Update a specific comment for a task
// @Tags tasks
// @Accept json
// @Produce json
// @Param id path string true "Task ID"
// @Param comment_id path string true "Comment ID"
// @Param comment body object true "Comment data" {"text": "string"}
// @Success 200 {object} models.Comment
// @Failure 400 {object} map[string]string "error response"
// @Failure 404 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/{id}/comments/{comment_id} [put]
func (h *TaskHandler) UpdateComment(c *gin.Context) {
	taskID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input")})
		return
	}

	commentID, err := uuid.Parse(c.Param("comment_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input")})
		return
	}

	var input struct {
		Text string `json:"text" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input")})
		return
	}

	userID := c.GetString("user_id")
	comment, err := h.Service.UpdateComment(c.Request.Context(), taskID, commentID, userID, input.Text)
	if err != nil {
		if err.Error() == "comment not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Comment not found"})
		} else if err.Error() == "permission denied" {
			c.JSON(http.StatusForbidden, gin.H{"error": h.getErrorMessage("permission_denied")})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error")})
		}
		return
	}

	c.JSON(http.StatusOK, comment)
}

// DeleteComment godoc
// @Summary Delete a comment
// @Description Delete a specific comment from a task
// @Tags tasks
// @Produce json
// @Param id path string true "Task ID"
// @Param comment_id path string true "Comment ID"
// @Success 200 {object} map[string]string "message response"
// @Failure 400 {object} map[string]string "error response"
// @Failure 404 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/{id}/comments/{comment_id} [delete]
func (h *TaskHandler) DeleteComment(c *gin.Context) {
	taskID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input")})
		return
	}

	commentID, err := uuid.Parse(c.Param("comment_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input")})
		return
	}

	userID := c.GetString("user_id")
	if err := h.Service.DeleteComment(c.Request.Context(), taskID, commentID, userID); err != nil {
		if err.Error() == "comment not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Comment not found"})
		} else if err.Error() == "permission denied" {
			c.JSON(http.StatusForbidden, gin.H{"error": h.getErrorMessage("permission_denied")})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error")})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Comment deleted"})
}

func (h *TaskHandler) CreateFollowUpTask(c *gin.Context) {
	var input struct {
		ClientID     string `json:"client_id" binding:"required"`
		FollowUpDate string `json:"follow_up_date" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input")})
		return
	}
	dueDate, err := time.Parse("2006-01-02", input.FollowUpDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date format"})
		return
	}
	userID := c.GetString("user_id")
	task := models.Task{
		ID:          uuid.New(),
		Title:       "Follow-up с клиентом " + input.ClientID,
		Description: "Связаться с клиентом для продолжения продаж",
		Status:      "pending",
		DueDate:     dueDate,
		CreatedBy:   userID,
		CreatedAt:   time.Now(),
	}
	createdTask, err := h.Service.CreateTask(c.Request.Context(), userID, &task, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error")})
		return
	}
	c.JSON(http.StatusCreated, createdTask)
}

func (h *TaskHandler) GetSalesAnalytics(c *gin.Context) {
	userID := c.GetString("user_id")
	period := c.Query("period")
	analytics, err := h.Service.GetSalesAnalytics(c.Request.Context(), userID, period)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error")})
		return
	}
	c.JSON(http.StatusOK, analytics)
}

// StartTimeTracking godoc
// @Summary Start time tracking for a task
// @Description Start tracking time for a specific task
// @Tags time_tracking
// @Produce json
// @Param task_id path string true "Task ID"
// @Success 200 {object} models.TimeEntry
// @Failure 400 {object} map[string]string "error response"
// @Failure 403 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/{task_id}/time/start [post]
func (h *TaskHandler) StartTimeTracking(c *gin.Context) {
	userID := c.GetString("user_id")
	role := c.GetString("role")
	if role == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": h.getErrorMessage("permission_denied")})
		return
	}

	taskID, err := uuid.Parse(c.Param("task_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input")})
		return
	}
	entry, err := h.Service.StartTimeTracking(c.Request.Context(), userID, taskID.String())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, entry)
}

// StopTimeTracking godoc
// @Summary Stop time tracking
// @Description Stop tracking time for a specific entry
// @Tags time_tracking
// @Produce json
// @Param entry_id query string true "Time Entry ID"
// @Success 200 {object} models.TimeEntry
// @Failure 400 {object} map[string]string "error response"
// @Failure 403 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/time/stop [post]
func (h *TaskHandler) StopTimeTracking(c *gin.Context) {
	userID := c.GetString("user_id")
	role := c.GetString("role")
	if role == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": h.getErrorMessage("permission_denied")})
		return
	}

	entryID := c.Query("entry_id")
	if entryID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input") + ": entry_id is required"})
		return
	}
	entry, err := h.Service.StopTimeTracking(c.Request.Context(), entryID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, entry)
}

// GetActiveTimeEntries godoc
// @Summary Get active time entries for a user
// @Description Retrieve all active time tracking entries for the current user
// @Tags time_tracking
// @Produce json
// @Success 200 {array} models.TimeEntry
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/time/active [get]
func (h *TaskHandler) GetActiveTimeEntries(c *gin.Context) {
	userID := c.GetString("user_id")
	entries, err := h.Service.GetActiveTimeEntries(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error")})
		return
	}
	c.JSON(http.StatusOK, entries)
}

// GetTotalActiveTime godoc
// @Summary Get total active time for a user
// @Description Calculate total active time in minutes for the current user
// @Tags time_tracking
// @Produce json
// @Success 200 {object} map[string]int "total_minutes response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/time/total [get]
func (h *TaskHandler) GetTotalActiveTime(c *gin.Context) {
	userID := c.GetString("user_id")
	totalMinutes, err := h.Service.CalculateTotalActiveTime(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error")})
		return
	}
	c.JSON(http.StatusOK, gin.H{"total_minutes": totalMinutes})
}

// GetTimeReport godoc
// @Summary Get time report for a user
// @Description Retrieve time tracking report for a user
// @Tags time_tracking
// @Produce json
// @Param user_id path string true "User ID"
// @Param period query string false "Period (day, week, month)"
// @Success 200 {array} models.TimeEntry
// @Failure 403 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/users/{user_id}/time-report [get]
func (h *TaskHandler) GetTimeReport(c *gin.Context) {
	role := c.GetString("role")
	if role != "manager" && role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": h.getErrorMessage("permission_denied")})
		return
	}

	userID := c.Param("user_id")
	period := c.Query("period")
	entries, err := h.Service.GetTimeReport(c.Request.Context(), userID, period)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error")})
		return
	}
	c.JSON(http.StatusOK, entries)
}

// CalculatePayroll godoc
// @Summary Calculate payroll for a user
// @Description Calculate payroll based on tracked time
// @Tags payroll
// @Produce json
// @Param period query string true "Period (e.g., 2025-03)"
// @Success 200 {object} models.Payroll
// @Failure 400 {object} map[string]string "error response"
// @Failure 403 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/payroll/calculate [post]
func (h *TaskHandler) CalculatePayroll(c *gin.Context) {
	role := c.GetString("role")
	if role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": h.getErrorMessage("permission_denied")})
		return
	}

	userID := c.GetString("user_id")
	period := c.Query("period")
	if period == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input") + ": period is required"})
		return
	}
	payroll, err := h.Service.CalculatePayroll(c.Request.Context(), userID, period)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error")})
		return
	}
	c.JSON(http.StatusOK, payroll)
}

// GetAggregatePayroll godoc
// @Summary Get aggregate payroll data
// @Description Retrieve aggregated payroll data for all employees in the company
// @Tags payroll
// @Produce json
// @Param period query string true "Period (e.g., 2025-03)"
// @Success 200 {array} map[string]interface{} "aggregate payroll data"
// @Failure 400 {object} map[string]string "error response"
// @Failure 403 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/payroll/aggregate [get]
func (h *TaskHandler) GetAggregatePayroll(c *gin.Context) {
	userID := c.GetString("user_id")
	role := c.GetString("role")
	if role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": h.getErrorMessage("permission_denied")})
		return
	}

	period := c.Query("period")
	if period == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input") + ": period is required"})
		return
	}

	payrollData, err := h.Service.GetAggregatePayroll(c.Request.Context(), userID, period)
	if err != nil {
		if err.Error() == "only admins can access aggregate payroll data" {
			c.JSON(http.StatusForbidden, gin.H{"error": h.getErrorMessage("permission_denied")})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error") + ": " + err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, payrollData)
}

// CreateOnboardingChecklist godoc
// @Summary Create onboarding checklist for a user
// @Description Generate onboarding tasks for a new user
// @Tags onboarding
// @Produce json
// @Success 200 {object} map[string]string "message response"
// @Failure 403 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/onboarding [post]
func (h *TaskHandler) CreateOnboardingChecklist(c *gin.Context) {
	role := c.GetString("role")
	if role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": h.getErrorMessage("permission_denied")})
		return
	}

	userID := c.GetString("user_id")
	if err := h.Service.CreateOnboardingChecklist(c.Request.Context(), userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error")})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Onboarding checklist created"})
}

// CreateRecurringTask godoc
// @Summary Create a recurring task
// @Description Create a task that repeats on a specified interval
// @Tags tasks
// @Accept json
// @Produce json
// @Param request body object true "Task with recurrence" {"title": "string", "description": "string", "due_date": "string", "priority": "int", "recurrence": {"interval": "string", "until": "string"}}
// @Success 201 {object} models.Task
// @Failure 400 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/recurring [post]
func (h *TaskHandler) CreateRecurringTask(c *gin.Context) {
	var input struct {
		models.Task
		Recurrence *services.Recurrence `json:"recurrence"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input") + ": " + err.Error()})
		return
	}
	if input.Title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input") + ": title is required"})
		return
	}

	userID := c.GetString("user_id")
	input.ID = uuid.New()
	input.CreatedAt = time.Now()
	input.Status = "pending"

	task, err := h.Service.CreateRecurringTask(c.Request.Context(), userID, &input.Task, input.Recurrence)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error") + ": " + err.Error()})
		return
	}
	c.JSON(http.StatusCreated, task)
}

// GetProductivityReport godoc
// @Summary Get productivity report
// @Description Retrieve productivity report for a user over a period
// @Tags time_tracking
// @Produce json
// @Param period query string true "Period (day, week, month)"
// @Success 200 {object} map[string]interface{} "productivity report"
// @Failure 400 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/productivity [get]
func (h *TaskHandler) GetProductivityReport(c *gin.Context) {
	userID := c.GetString("user_id")
	period := c.Query("period")
	if period != "day" && period != "week" && period != "month" {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input") + ": period must be day, week, or month"})
		return
	}

	report, err := h.Service.GetProductivityReport(c.Request.Context(), userID, period)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error") + ": " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, report)
}

// CreateTaxReminders godoc
// @Summary Create tax reminders
// @Description Generate tax and report reminders based on company type
// @Tags taxes
// @Accept json
// @Produce json
// @Param request body object true "Company type" {"company_type": "string"}
// @Success 200 {object} map[string]string "message response"
// @Failure 400 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/tax-reminders [post]
func (h *TaskHandler) CreateTaxReminders(c *gin.Context) {
	userID := c.GetString("user_id")
	var input struct {
		CompanyType string `json:"company_type" binding:"required,oneof=self-employed ip"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input") + ": " + err.Error()})
		return
	}

	if err := h.Service.CreateTaxReminders(c.Request.Context(), userID, input.CompanyType); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error") + ": " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Tax reminders created"})
}

// CreateLeaveRequest godoc
// @Summary Create a leave request
// @Description Submit a vacation or sick leave request
// @Tags leave
// @Accept json
// @Produce json
// @Param request body object true "Leave request data" {"type": "string", "start_date": "string", "end_date": "string"}
// @Success 200 {object} models.LeaveRequest
// @Failure 400 {object} map[string]string "error response"
// @Failure 403 {object} map[string]string "error response"
// @Failure 500 {object} map[string]string "error response"
// @Router /tasks/leave/request [post]
func (h *TaskHandler) CreateLeaveRequest(c *gin.Context) {
	userID := c.GetString("user_id")
	role := c.GetString("role")
	if role == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": h.getErrorMessage("permission_denied")})
		return
	}

	var input struct {
		Type      string `json:"type" binding:"required,oneof=vacation sick"`
		StartDate string `json:"start_date" binding:"required"`
		EndDate   string `json:"end_date" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input") + ": " + err.Error()})
		return
	}
	startDate, err := time.Parse("2006-01-02", input.StartDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input") + ": invalid start_date"})
		return
	}
	endDate, err := time.Parse("2006-01-02", input.EndDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": h.getErrorMessage("invalid_input") + ": invalid end_date"})
		return
	}
	request, err := h.Service.CreateLeaveRequest(c.Request.Context(), userID, input.Type, startDate, endDate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error")})
		return
	}
	c.JSON(http.StatusOK, request)
}
