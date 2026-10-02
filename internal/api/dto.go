package api

import (
	"context"
	"time"

	"github.com/google/uuid"

	"clerkai/task-service/internal/models"
	"clerkai/task-service/internal/params"
)

// TaskDTO — Task из docs/API.md.
type TaskDTO struct {
	ID            uuid.UUID  `json:"id"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Status        string     `json:"status"`
	Priority      string     `json:"priority"`
	DueDate       *time.Time `json:"due_date"`
	AssigneeID    *uuid.UUID `json:"assignee_id"`
	AssigneeName  *string    `json:"assignee_name"`
	CreatedBy     uuid.UUID  `json:"created_by"`
	CreatedByName string     `json:"created_by_name"`
	Tags          []string   `json:"tags"`
	CompanyID     uuid.UUID  `json:"company_id"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	CompletedAt   *time.Time `json:"completed_at"`
	CommentsCount int64      `json:"comments_count"`
	TotalMinutes  int64      `json:"total_minutes"`
}

func taskDTO(t *models.Task, comments, minutes int64) TaskDTO {
	return TaskDTO{
		ID:            t.ID,
		Title:         t.Title,
		Description:   t.Description,
		Status:        t.Status,
		Priority:      t.Priority,
		DueDate:       tsp(t.DueDate),
		AssigneeID:    t.AssigneeID,
		AssigneeName:  t.AssigneeName,
		CreatedBy:     t.CreatedBy,
		CreatedByName: t.CreatedByName,
		Tags:          nonNil(t.Tags),
		CompanyID:     t.CompanyID,
		CreatedAt:     ts(t.CreatedAt),
		UpdatedAt:     ts(t.UpdatedAt),
		CompletedAt:   tsp(t.CompletedAt),
		CommentsCount: comments,
		TotalMinutes:  minutes,
	}
}

// CommentDTO — Comment из docs/API.md.
type CommentDTO struct {
	ID         uuid.UUID `json:"id"`
	TaskID     uuid.UUID `json:"task_id"`
	AuthorID   uuid.UUID `json:"author_id"`
	AuthorName string    `json:"author_name"`
	Text       string    `json:"text"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func commentDTO(m *models.Comment) CommentDTO {
	return CommentDTO{
		ID: m.ID, TaskID: m.TaskID, AuthorID: m.AuthorID, AuthorName: m.AuthorName, Text: m.Text,
		CreatedAt: ts(m.CreatedAt), UpdatedAt: ts(m.UpdatedAt),
	}
}

// TimeEntryDTO — TimeEntry из docs/API.md. Для активного таймера duration_minutes — прошедшее время.
type TimeEntryDTO struct {
	ID              uuid.UUID  `json:"id"`
	TaskID          uuid.UUID  `json:"task_id"`
	TaskTitle       string     `json:"task_title"`
	UserID          uuid.UUID  `json:"user_id"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at"`
	DurationMinutes int        `json:"duration_minutes"`
}

func timeEntryDTO(e *models.TimeEntry, title string, now time.Time) TimeEntryDTO {
	minutes := e.DurationMinutes
	if e.EndedAt == nil {
		minutes = params.DurationMinutes(e.StartedAt, now)
	}
	return TimeEntryDTO{
		ID: e.ID, TaskID: e.TaskID, TaskTitle: title, UserID: e.UserID,
		StartedAt: ts(e.StartedAt), EndedAt: tsp(e.EndedAt), DurationMinutes: minutes,
	}
}

// MeetingDTO — Meeting из docs/API.md.
type MeetingDTO struct {
	ID           uuid.UUID  `json:"id"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	StartAt      time.Time  `json:"start_at"`
	EndAt        time.Time  `json:"end_at"`
	Location     string     `json:"location"`
	Participants []string   `json:"participants"`
	ClientID     *uuid.UUID `json:"client_id"`
	CreatedBy    uuid.UUID  `json:"created_by"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func meetingDTO(m *models.Meeting) MeetingDTO {
	return MeetingDTO{
		ID: m.ID, Title: m.Title, Description: m.Description,
		StartAt: ts(m.StartAt), EndAt: ts(m.EndAt), Location: m.Location,
		Participants: nonNil(m.Participants), ClientID: m.ClientID, CreatedBy: m.CreatedBy,
		CreatedAt: ts(m.CreatedAt), UpdatedAt: ts(m.UpdatedAt),
	}
}

// ImportantDateDTO — ImportantDate из docs/API.md; date — строка YYYY-MM-DD.
// created_by — автор (v2.1); у старых записей без автора — нулевой UUID.
type ImportantDateDTO struct {
	ID               uuid.UUID `json:"id"`
	Title            string    `json:"title"`
	Date             string    `json:"date"`
	Type             string    `json:"type"`
	RemindDaysBefore int       `json:"remind_days_before"`
	Description      string    `json:"description"`
	CreatedBy        uuid.UUID `json:"created_by"`
	CreatedAt        time.Time `json:"created_at"`
}

func importantDateDTO(d *models.ImportantDate) ImportantDateDTO {
	return ImportantDateDTO{
		ID: d.ID, Title: d.Title, Date: FormatDate(d.Date), Type: d.Type,
		RemindDaysBefore: d.RemindDaysBefore, Description: d.Description, CreatedBy: d.CreatedBy,
		CreatedAt: ts(d.CreatedAt),
	}
}

// FormatDate выводит календарную дату без сдвига часового пояса.
// Значение колонки DATE драйвер отдаёт как полночь (UTC или локальную) — берём компоненты как есть.
func FormatDate(t time.Time) string {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).Format(params.DateLayout)
}

// --- агрегаты для задач (без N+1: один GROUP BY на страницу) ---

type taskAgg struct {
	TaskID uuid.UUID
	N      int64
}

const commentsAggSQL = `SELECT task_id, COUNT(*) AS n FROM comments
WHERE company_id = ? AND task_id IN ? GROUP BY task_id`

const minutesAggSQL = `SELECT task_id,
  COALESCE(SUM(CASE WHEN ended_at IS NULL
                    THEN GREATEST(0, FLOOR(EXTRACT(EPOCH FROM (NOW() - started_at)) / 60))
                    ELSE duration_minutes END), 0)::bigint AS n
FROM time_entries WHERE company_id = ? AND task_id IN ? GROUP BY task_id`

func (h *Handler) aggregate(ctx context.Context, sql string, companyID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]int64, error) {
	out := make(map[uuid.UUID]int64, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []taskAgg
	if err := h.db.WithContext(ctx).Raw(sql, companyID, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.TaskID] = r.N
	}
	return out, nil
}

// taskDTOs собирает DTO для набора задач с comments_count и total_minutes.
func (h *Handler) taskDTOs(ctx context.Context, companyID uuid.UUID, tasks []models.Task) ([]TaskDTO, error) {
	ids := make([]uuid.UUID, len(tasks))
	for i := range tasks {
		ids[i] = tasks[i].ID
	}
	comments, err := h.aggregate(ctx, commentsAggSQL, companyID, ids)
	if err != nil {
		return nil, err
	}
	minutes, err := h.aggregate(ctx, minutesAggSQL, companyID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]TaskDTO, len(tasks))
	for i := range tasks {
		out[i] = taskDTO(&tasks[i], comments[tasks[i].ID], minutes[tasks[i].ID])
	}
	return out, nil
}

func (h *Handler) oneTaskDTO(ctx context.Context, t *models.Task) (TaskDTO, error) {
	list, err := h.taskDTOs(ctx, t.CompanyID, []models.Task{*t})
	if err != nil {
		return TaskDTO{}, err
	}
	return list[0], nil
}

// taskTitles — названия задач компании по id одним запросом.
func (h *Handler) taskTitles(ctx context.Context, companyID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var tasks []models.Task
	err := h.db.WithContext(ctx).Model(&models.Task{}).Select("id", "title").
		Where("company_id = ? AND id IN ?", companyID, ids).Find(&tasks).Error
	if err != nil {
		return nil, err
	}
	for i := range tasks {
		out[tasks[i].ID] = tasks[i].Title
	}
	return out, nil
}
