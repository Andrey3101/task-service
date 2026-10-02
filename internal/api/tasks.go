package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"clerkai/task-service/internal/auth"
	"clerkai/task-service/internal/models"
	"clerkai/task-service/internal/notify"
	"clerkai/task-service/internal/params"
)

const (
	msgTaskNotFound = "Задача не найдена"
	msgForbidden    = "Недостаточно прав"
)

// taskInput — поля задачи для POST/PUT (PUT — частичное обновление).
type taskInput struct {
	Title        Opt[string]   `json:"title"`
	Description  Opt[string]   `json:"description"`
	Status       Opt[string]   `json:"status"`
	Priority     Opt[string]   `json:"priority"`
	DueDate      Opt[string]   `json:"due_date"`
	AssigneeID   Opt[string]   `json:"assignee_id"`
	AssigneeName Opt[string]   `json:"assignee_name"`
	Tags         Opt[[]string] `json:"tags"`
}

// applyTaskInput переносит поля в модель. Возвращает признак смены исполнителя
// или (field, msg) ошибки валидации.
func applyTaskInput(t *models.Task, in taskInput, u auth.User, creating bool) (assigneeChanged bool, field, msg string) {
	if creating && !in.Title.Set {
		return false, "title", "обязательное поле"
	}
	if in.Title.Set {
		title, ok := params.CleanText(in.Title.V, 1, 255)
		if in.Title.Null || !ok {
			return false, "title", "от 1 до 255 символов"
		}
		t.Title = title
	}
	if in.Description.Set {
		desc, ok := params.CleanText(in.Description.V, 0, 10000)
		if !ok {
			return false, "description", "не длиннее 10000 символов"
		}
		t.Description = desc
	}
	if in.Status.Set {
		if in.Status.Null || !params.OneOf(in.Status.V, params.TaskStatuses) {
			return false, "status", "допустимо todo, in_progress, done, cancelled"
		}
		t.Status = in.Status.V
	}
	if in.Priority.Set {
		if in.Priority.Null || !params.OneOf(in.Priority.V, params.TaskPriorities) {
			return false, "priority", "допустимо low, medium, high, urgent"
		}
		t.Priority = in.Priority.V
	}
	if in.DueDate.Set {
		due, err := optDateTime(in.DueDate)
		if err != nil {
			return false, "due_date", err.Error()
		}
		t.DueDate = due
	}
	if in.AssigneeID.Set {
		id, err := optUUID(in.AssigneeID)
		if err != nil {
			return false, "assignee_id", err.Error()
		}
		assigneeChanged = !sameUUID(t.AssigneeID, id)
		t.AssigneeID = id
		if assigneeChanged && !in.AssigneeName.Set {
			t.AssigneeName = nil
		}
	}
	if in.AssigneeName.Set {
		name, ok := params.CleanText(in.AssigneeName.V, 0, 255)
		if !ok {
			return false, "assignee_name", "не длиннее 255 символов"
		}
		if in.AssigneeName.Null || name == "" {
			t.AssigneeName = nil
		} else {
			t.AssigneeName = &name
		}
	}
	if t.AssigneeID == nil {
		t.AssigneeName = nil
	} else if t.AssigneeName == nil && *t.AssigneeID == u.ID && u.Name != "" {
		name := u.Name
		t.AssigneeName = &name
	}
	if in.Tags.Set {
		tags, ok := params.CleanList(in.Tags.V, 20, 50)
		if !ok {
			return false, "tags", "не более 20 тегов длиной до 50 символов"
		}
		t.Tags = tags
	}
	return assigneeChanged, "", ""
}

func sameUUID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// syncCompletedAt выставляет completed_at при переходе в done и сбрасывает при выходе из него.
func (h *Handler) syncCompletedAt(t *models.Task) {
	if t.Status == "done" {
		if t.CompletedAt == nil {
			now := h.now()
			t.CompletedAt = &now
		}
	} else {
		t.CompletedAt = nil
	}
}

func canModifyTask(u auth.User, t *models.Task) bool {
	if u.IsAdmin() || t.CreatedBy == u.ID {
		return true
	}
	return t.AssigneeID != nil && *t.AssigneeID == u.ID
}

func (h *Handler) notifyAssigned(u auth.User, t *models.Task) {
	if t.AssigneeID == nil || *t.AssigneeID == u.ID {
		return
	}
	who := u.Name
	if who == "" {
		who = "Коллега"
	}
	h.notifier.SendAsync(notify.Notification{
		UserID:    *t.AssigneeID,
		CompanyID: t.CompanyID,
		Title:     "Вам назначена задача",
		Message:   fmt.Sprintf("%s назначил(а) вам задачу «%s»", who, t.Title),
		Type:      "task",
		Link:      "/tasks/" + t.ID.String(),
	})
}

// findTask ищет задачу в пределах компании; чужая или несуществующая — 404.
func (h *Handler) findTask(c *gin.Context, u auth.User) (*models.Task, bool) {
	id, ok := pathID(c, "id", msgTaskNotFound)
	if !ok {
		return nil, false
	}
	var t models.Task
	err := h.db.WithContext(c.Request.Context()).Where("company_id = ? AND id = ?", u.CompanyID, id).First(&t).Error
	if isNotFound(err) {
		fail(c, http.StatusNotFound, msgTaskNotFound)
		return nil, false
	}
	if err != nil {
		h.internalError(c, err)
		return nil, false
	}
	return &t, true
}

// GET /api/tasks
func (h *Handler) listTasks(c *gin.Context, u auth.User) {
	pg, limit, ok := pagination(c)
	if !ok {
		return
	}
	var statuses, priorities []string
	if s := c.Query("status"); s != "" {
		for _, v := range strings.Split(s, ",") {
			if v = strings.TrimSpace(v); !params.OneOf(v, params.TaskStatuses) {
				invalid(c, "status", "допустимо todo, in_progress, done, cancelled")
				return
			}
			statuses = append(statuses, v)
		}
	}
	if s := c.Query("priority"); s != "" {
		for _, v := range strings.Split(s, ",") {
			if v = strings.TrimSpace(v); !params.OneOf(v, params.TaskPriorities) {
				invalid(c, "priority", "допустимо low, medium, high, urgent")
				return
			}
			priorities = append(priorities, v)
		}
	}
	var assignee *uuid.UUID
	if s := c.Query("assignee_id"); s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			invalid(c, "assignee_id", "ожидается UUID")
			return
		}
		assignee = &id
	}
	search := strings.TrimSpace(c.Query("search"))
	if len([]rune(search)) > 200 {
		invalid(c, "search", "не длиннее 200 символов")
		return
	}
	mine := c.Query("mine") == "true" || c.Query("mine") == "1"
	dueFrom, dueTo, ok := queryRange(c, "due_from", "due_to")
	if !ok {
		return
	}
	order, ok := params.TaskSortOrder(c.Query("sort"))
	if !ok {
		invalid(c, "sort", "допустимо created_at, -created_at, due_date, -due_date, priority, -updated_at")
		return
	}

	ctx := c.Request.Context()
	query := func() *gorm.DB {
		q := h.db.WithContext(ctx).Model(&models.Task{}).Where("company_id = ?", u.CompanyID)
		if len(statuses) > 0 {
			q = q.Where("status IN ?", statuses)
		}
		if len(priorities) > 0 {
			q = q.Where("priority IN ?", priorities)
		}
		if assignee != nil {
			q = q.Where("assignee_id = ?", *assignee)
		}
		if search != "" {
			pattern := params.LikePattern(search)
			q = q.Where("(title ILIKE ? OR description ILIKE ?)", pattern, pattern)
		}
		if mine {
			q = q.Where("(assignee_id = ? OR created_by = ?)", u.ID, u.ID)
		}
		if dueFrom != nil {
			q = q.Where("due_date >= ?", *dueFrom)
		}
		if dueTo != nil {
			q = q.Where("due_date < ?", *dueTo)
		}
		return q
	}

	var total int64
	if err := query().Count(&total).Error; err != nil {
		h.internalError(c, err)
		return
	}
	var tasks []models.Task
	if err := query().Order(order).Limit(limit).Offset(params.Offset(pg, limit)).Find(&tasks).Error; err != nil {
		h.internalError(c, err)
		return
	}
	dtos, err := h.taskDTOs(ctx, u.CompanyID, tasks)
	if err != nil {
		h.internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, page[TaskDTO]{Items: dtos, Total: total, Page: pg, Limit: limit})
}

// POST /api/tasks
func (h *Handler) createTask(c *gin.Context, u auth.User) {
	var in taskInput
	if !bindJSON(c, &in, false) {
		return
	}
	t := newTask(u)
	if _, field, msg := applyTaskInput(&t, in, u, true); field != "" {
		invalid(c, field, msg)
		return
	}
	h.syncCompletedAt(&t)
	if err := h.db.WithContext(c.Request.Context()).Create(&t).Error; err != nil {
		h.internalError(c, err)
		return
	}
	h.notifyAssigned(u, &t)
	c.JSON(http.StatusCreated, taskDTO(&t, 0, 0))
}

func newTask(u auth.User) models.Task {
	return models.Task{
		CompanyID:     u.CompanyID,
		Status:        "todo",
		Priority:      "medium",
		CreatedBy:     u.ID,
		CreatedByName: u.Name,
		Tags:          []string{},
	}
}

// GET /api/tasks/:id
func (h *Handler) getTask(c *gin.Context, u auth.User) {
	t, ok := h.findTask(c, u)
	if !ok {
		return
	}
	dto, err := h.oneTaskDTO(c.Request.Context(), t)
	if err != nil {
		h.internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto)
}

// PUT /api/tasks/:id — частичное обновление.
func (h *Handler) updateTask(c *gin.Context, u auth.User) {
	t, ok := h.findTask(c, u)
	if !ok {
		return
	}
	if !canModifyTask(u, t) {
		fail(c, http.StatusForbidden, msgForbidden)
		return
	}
	var in taskInput
	if !bindJSON(c, &in, false) {
		return
	}
	assigneeChanged, field, msg := applyTaskInput(t, in, u, false)
	if field != "" {
		invalid(c, field, msg)
		return
	}
	h.syncCompletedAt(t)
	if t.Tags == nil {
		t.Tags = []string{}
	}
	if err := h.db.WithContext(c.Request.Context()).Save(t).Error; err != nil {
		h.internalError(c, err)
		return
	}
	if assigneeChanged {
		h.notifyAssigned(u, t)
	}
	dto, err := h.oneTaskDTO(c.Request.Context(), t)
	if err != nil {
		h.internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto)
}

// DELETE /api/tasks/:id — вместе с комментариями, учётом времени и отметками напоминаний.
func (h *Handler) deleteTask(c *gin.Context, u auth.User) {
	t, ok := h.findTask(c, u)
	if !ok {
		return
	}
	if !canModifyTask(u, t) {
		fail(c, http.StatusForbidden, msgForbidden)
		return
	}
	err := h.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		for _, m := range []any{&models.Comment{}, &models.TimeEntry{}, &models.TaskReminder{}} {
			if err := tx.Where("company_id = ? AND task_id = ?", u.CompanyID, t.ID).Delete(m).Error; err != nil {
				return err
			}
		}
		return tx.Where("company_id = ? AND id = ?", u.CompanyID, t.ID).Delete(&models.Task{}).Error
	})
	if err != nil {
		h.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
