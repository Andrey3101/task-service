package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"clerkai/task-service/internal/auth"
	"clerkai/task-service/internal/models"
	"clerkai/task-service/internal/notify"
	"clerkai/task-service/internal/params"
	"clerkai/task-service/internal/taxcal"
)

var openStatuses = []string{"todo", "in_progress"}

func startOfDayUTC(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// startOfWeekUTC — понедельник 00:00 UTC текущей недели.
func startOfWeekUTC(t time.Time) time.Time {
	day := startOfDayUTC(t)
	offset := (int(day.Weekday()) + 6) % 7 // Monday=0 … Sunday=6
	return day.AddDate(0, 0, -offset)
}

type statsRow struct {
	Total             int64
	Todo              int64
	InProgress        int64
	Done              int64
	Cancelled         int64
	Low               int64
	Medium            int64
	High              int64
	Urgent            int64
	Overdue           int64
	DueToday          int64
	CompletedThisWeek int64
}

const statsSQL = `SELECT
  COUNT(*) AS total,
  COUNT(*) FILTER (WHERE status = 'todo') AS todo,
  COUNT(*) FILTER (WHERE status = 'in_progress') AS in_progress,
  COUNT(*) FILTER (WHERE status = 'done') AS done,
  COUNT(*) FILTER (WHERE status = 'cancelled') AS cancelled,
  COUNT(*) FILTER (WHERE priority = 'low') AS low,
  COUNT(*) FILTER (WHERE priority = 'medium') AS medium,
  COUNT(*) FILTER (WHERE priority = 'high') AS high,
  COUNT(*) FILTER (WHERE priority = 'urgent') AS urgent,
  COUNT(*) FILTER (WHERE status IN ('todo', 'in_progress') AND due_date < ?) AS overdue,
  COUNT(*) FILTER (WHERE status IN ('todo', 'in_progress') AND due_date >= ? AND due_date < ?) AS due_today,
  COUNT(*) FILTER (WHERE status = 'done' AND completed_at >= ?) AS completed_this_week
FROM tasks WHERE company_id = ?`

// GET /api/tasks/stats
func (h *Handler) taskStats(c *gin.Context, u auth.User) {
	now := h.now()
	today := startOfDayUTC(now)
	var r statsRow
	err := h.db.WithContext(c.Request.Context()).
		Raw(statsSQL, now, today, today.AddDate(0, 0, 1), startOfWeekUTC(now), u.CompanyID).
		Scan(&r).Error
	if err != nil {
		h.internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"total":               r.Total,
		"by_status":           gin.H{"todo": r.Todo, "in_progress": r.InProgress, "done": r.Done, "cancelled": r.Cancelled},
		"by_priority":         gin.H{"low": r.Low, "medium": r.Medium, "high": r.High, "urgent": r.Urgent},
		"overdue":             r.Overdue,
		"due_today":           r.DueToday,
		"completed_this_week": r.CompletedThisWeek,
	})
}

type dayCount struct {
	Day string
	N   int64
}

type activityItem struct {
	Date      string `json:"date"`
	Created   int64  `json:"created"`
	Completed int64  `json:"completed"`
}

// GET /api/tasks/activity?days=30
func (h *Handler) taskActivity(c *gin.Context, u auth.User) {
	days := 30
	if s := c.Query("days"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 365 {
			invalid(c, "days", "целое число от 1 до 365")
			return
		}
		days = n
	}
	since := startOfDayUTC(h.now()).AddDate(0, 0, -(days - 1))
	ctx := c.Request.Context()

	var created, completed []dayCount
	err := h.db.WithContext(ctx).Raw(`SELECT to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD') AS day, COUNT(*) AS n
FROM tasks WHERE company_id = ? AND created_at >= ? GROUP BY 1`, u.CompanyID, since).Scan(&created).Error
	if err == nil {
		err = h.db.WithContext(ctx).Raw(`SELECT to_char(completed_at AT TIME ZONE 'UTC', 'YYYY-MM-DD') AS day, COUNT(*) AS n
FROM tasks WHERE company_id = ? AND completed_at >= ? GROUP BY 1`, u.CompanyID, since).Scan(&completed).Error
	}
	if err != nil {
		h.internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, items[activityItem]{Items: buildActivity(since, days, created, completed)})
}

// buildActivity заполняет все дни периода (дни без событий — нули), по возрастанию даты.
func buildActivity(since time.Time, days int, created, completed []dayCount) []activityItem {
	cr := make(map[string]int64, len(created))
	for _, r := range created {
		cr[r.Day] = r.N
	}
	co := make(map[string]int64, len(completed))
	for _, r := range completed {
		co[r.Day] = r.N
	}
	out := make([]activityItem, 0, days)
	for i := 0; i < days; i++ {
		d := since.AddDate(0, 0, i).Format(params.DateLayout)
		out = append(out, activityItem{Date: d, Created: cr[d], Completed: co[d]})
	}
	return out
}

type suggestion struct {
	TaskID uuid.UUID `json:"task_id"`
	Title  string    `json:"title"`
	Reason string    `json:"reason"`
}

// suggestionReason — почему задача требует внимания ("" — не требует).
func suggestionReason(t *models.Task, now time.Time) string {
	if t.Status != "todo" && t.Status != "in_progress" {
		return ""
	}
	if t.DueDate != nil {
		if t.DueDate.Before(now) {
			return "Просрочена"
		}
		if !t.DueDate.After(now.Add(48 * time.Hour)) {
			return "Срок истекает в ближайшие 2 дня"
		}
	}
	if t.Status == "todo" && (t.Priority == "urgent" || t.Priority == "high") {
		if t.Priority == "urgent" {
			return "Срочная задача ещё не начата"
		}
		return "Задача с высоким приоритетом ещё не начата"
	}
	return ""
}

// GET /api/tasks/suggestions — задачи текущего пользователя (исполнитель или автор).
func (h *Handler) taskSuggestions(c *gin.Context, u auth.User) {
	now := h.now()
	var tasks []models.Task
	err := h.db.WithContext(c.Request.Context()).
		Where("company_id = ? AND status IN ?", u.CompanyID, openStatuses).
		Where("(assignee_id = ? OR created_by = ?)", u.ID, u.ID).
		Where("(due_date <= ? OR (status = 'todo' AND priority IN ?))", now.Add(48*time.Hour), []string{"urgent", "high"}).
		Order("due_date ASC NULLS LAST, CASE priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 ELSE 2 END ASC, created_at ASC").
		Limit(20).
		Find(&tasks).Error
	if err != nil {
		h.internalError(c, err)
		return
	}
	out := make([]suggestion, 0, len(tasks))
	for i := range tasks {
		if reason := suggestionReason(&tasks[i], now); reason != "" {
			out = append(out, suggestion{TaskID: tasks[i].ID, Title: tasks[i].Title, Reason: reason})
		}
	}
	c.JSON(http.StatusOK, items[suggestion]{Items: out})
}

type bulkAssignInput struct {
	TaskIDs      []string    `json:"task_ids"`
	AssigneeID   string      `json:"assignee_id"`
	AssigneeName Opt[string] `json:"assignee_name"`
}

// POST /api/tasks/bulk-assign — user меняет только свои задачи (автор/исполнитель), остальные пропускаются.
func (h *Handler) bulkAssign(c *gin.Context, u auth.User) {
	var in bulkAssignInput
	if !bindJSON(c, &in, false) {
		return
	}
	if len(in.TaskIDs) == 0 || len(in.TaskIDs) > 100 {
		invalid(c, "task_ids", "от 1 до 100 идентификаторов")
		return
	}
	ids := make([]uuid.UUID, 0, len(in.TaskIDs))
	for _, s := range in.TaskIDs {
		id, err := uuid.Parse(strings.TrimSpace(s))
		if err != nil {
			invalid(c, "task_ids", "ожидаются UUID")
			return
		}
		ids = append(ids, id)
	}
	assignee, err := uuid.Parse(strings.TrimSpace(in.AssigneeID))
	if err != nil {
		invalid(c, "assignee_id", "ожидается UUID")
		return
	}
	var name *string
	if v, ok := in.AssigneeName.Value(); ok {
		clean, valid := params.CleanText(v, 0, 255)
		if !valid {
			invalid(c, "assignee_name", "не длиннее 255 символов")
			return
		}
		if clean != "" {
			name = &clean
		}
	}
	if name == nil && assignee == u.ID && u.Name != "" {
		n := u.Name
		name = &n
	}

	ctx := c.Request.Context()
	var targets []models.Task
	q := h.db.WithContext(ctx).Model(&models.Task{}).Select("id", "title").
		Where("company_id = ? AND id IN ?", u.CompanyID, ids).
		Where("assignee_id IS DISTINCT FROM ?", assignee)
	if !u.IsAdmin() {
		q = q.Where("(created_by = ? OR assignee_id = ?)", u.ID, u.ID)
	}
	if err := q.Find(&targets).Error; err != nil {
		h.internalError(c, err)
		return
	}
	if len(targets) == 0 {
		c.JSON(http.StatusOK, gin.H{"updated": 0})
		return
	}
	targetIDs := make([]uuid.UUID, len(targets))
	for i := range targets {
		targetIDs[i] = targets[i].ID
	}
	var nameVal any // явный nil → NULL
	if name != nil {
		nameVal = *name
	}
	res := h.db.WithContext(ctx).Model(&models.Task{}).
		Where("company_id = ? AND id IN ?", u.CompanyID, targetIDs).
		Updates(map[string]any{"assignee_id": assignee, "assignee_name": nameVal, "updated_at": h.now()})
	if res.Error != nil {
		h.internalError(c, res.Error)
		return
	}
	if assignee != u.ID && res.RowsAffected > 0 {
		link, msg := "/tasks", fmt.Sprintf("Вам назначено задач: %d", res.RowsAffected)
		if len(targets) == 1 {
			link = "/tasks/" + targets[0].ID.String()
			msg = fmt.Sprintf("Вам назначена задача «%s»", targets[0].Title)
		}
		h.notifier.SendAsync(notify.Notification{
			UserID: assignee, CompanyID: u.CompanyID, Title: "Вам назначены задачи",
			Message: msg, Type: "task", Link: link,
		})
	}
	c.JSON(http.StatusOK, gin.H{"updated": res.RowsAffected})
}

type recurringInput struct {
	taskInput
	Recurrence string `json:"recurrence"`
	Count      int    `json:"count"`
	StartDate  string `json:"start_date"`
}

// POST /api/tasks/recurring — серия задач со сроками start_date + i*шаг.
func (h *Handler) createRecurring(c *gin.Context, u auth.User) {
	var in recurringInput
	if !bindJSON(c, &in, false) {
		return
	}
	if !params.OneOf(in.Recurrence, params.Recurrences) {
		invalid(c, "recurrence", "допустимо daily, weekly, monthly")
		return
	}
	if in.Count < 1 || in.Count > 52 {
		invalid(c, "count", "целое число от 1 до 52")
		return
	}
	if strings.TrimSpace(in.StartDate) == "" {
		invalid(c, "start_date", "обязательное поле")
		return
	}
	start, err := params.ParseDateTime(in.StartDate)
	if err != nil {
		invalid(c, "start_date", err.Error())
		return
	}
	in.DueDate = Opt[string]{} // срок задаёт start_date
	template := newTask(u)
	if _, field, msg := applyTaskInput(&template, in.taskInput, u, true); field != "" {
		invalid(c, field, msg)
		return
	}
	dates, err := params.RecurrenceDates(start, in.Recurrence, in.Count)
	if err != nil {
		fail(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	tasks := make([]models.Task, len(dates))
	for i, d := range dates {
		t := template
		t.Tags = append([]string{}, template.Tags...)
		due := d
		t.DueDate = &due
		h.syncCompletedAt(&t)
		tasks[i] = t
	}
	if err := h.db.WithContext(c.Request.Context()).Create(&tasks).Error; err != nil {
		h.internalError(c, err)
		return
	}
	if template.AssigneeID != nil && *template.AssigneeID != u.ID {
		h.notifier.SendAsync(notify.Notification{
			UserID: *template.AssigneeID, CompanyID: u.CompanyID, Title: "Вам назначены задачи",
			Message: fmt.Sprintf("Повторяющаяся задача «%s»: %d шт.", template.Title, len(tasks)),
			Type:    "task", Link: "/tasks/" + tasks[0].ID.String(),
		})
	}
	out := make([]TaskDTO, len(tasks))
	for i := range tasks {
		out[i] = taskDTO(&tasks[i], 0, 0)
	}
	c.JSON(http.StatusCreated, items[TaskDTO]{Items: out})
}

type taxRemindersInput struct {
	Year *int `json:"year"`
}

// POST /api/tasks/tax-reminders — задачи по налоговым срокам для company_type из токена.
// Повторный вызов не создаёт дубликатов (title + дата срока в пределах компании).
func (h *Handler) createTaxReminders(c *gin.Context, u auth.User) {
	var in taxRemindersInput
	if !bindJSON(c, &in, false) {
		return
	}
	if in.Year == nil {
		invalid(c, "year", "обязательное поле")
		return
	}
	deadlines, err := taxcal.Deadlines(u.CompanyType, *in.Year)
	if errors.Is(err, taxcal.ErrUnknownCompanyType) {
		fail(c, http.StatusBadRequest, "Неизвестный тип компании: напоминания доступны для self_employed, ip, ooo")
		return
	}
	if err != nil {
		invalid(c, "year", err.Error())
		return
	}

	titles := make([]string, len(deadlines))
	for i, d := range deadlines {
		titles[i] = d.Title
	}
	ctx := c.Request.Context()
	var existing []models.Task
	if err := h.db.WithContext(ctx).Select("id", "title", "due_date").
		Where("company_id = ? AND title IN ?", u.CompanyID, titles).
		Find(&existing).Error; err != nil {
		h.internalError(c, err)
		return
	}
	seen := make(map[string]struct{}, len(existing))
	for i := range existing {
		if existing[i].DueDate != nil {
			seen[existing[i].Title+"|"+existing[i].DueDate.UTC().Format(params.DateLayout)] = struct{}{}
		}
	}

	tasks := make([]models.Task, 0, len(deadlines))
	for _, d := range deadlines {
		if _, dup := seen[d.Title+"|"+d.Date.Format(params.DateLayout)]; dup {
			continue
		}
		due := params.DateAtNoon(d.Date)
		t := newTask(u)
		t.Title = d.Title
		t.Description = d.Description
		t.Priority = "high"
		t.DueDate = &due
		t.Tags = []string{"налоги"}
		tasks = append(tasks, t)
	}
	if len(tasks) > 0 {
		err := h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return tx.Create(&tasks).Error })
		if err != nil {
			h.internalError(c, err)
			return
		}
	}
	out := make([]TaskDTO, len(tasks))
	for i := range tasks {
		out[i] = taskDTO(&tasks[i], 0, 0)
	}
	c.JSON(http.StatusCreated, items[TaskDTO]{Items: out})
}
