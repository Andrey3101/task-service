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

	tasks, total, err := h.Service.GetTasks(c.Request.Context(), status, dueDate, lim, off)
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
	tasks, err := h.Service.GetMyTasks(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": h.getErrorMessage("server_error")})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tasks": tasks})
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
