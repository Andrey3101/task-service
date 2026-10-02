// Package models — GORM-модели task-service (PostgreSQL, tasks_db).
package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Task — задача компании.
type Task struct {
	ID            uuid.UUID  `gorm:"type:uuid;primaryKey"`
	CompanyID     uuid.UUID  `gorm:"type:uuid;not null;index:idx_tasks_company_created,priority:1;index:idx_tasks_company_status,priority:1;index:idx_tasks_company_due,priority:1"`
	Title         string     `gorm:"size:255;not null"`
	Description   string     `gorm:"type:text;not null"`
	Status        string     `gorm:"size:20;not null;index:idx_tasks_company_status,priority:2"`
	Priority      string     `gorm:"size:20;not null"`
	DueDate       *time.Time `gorm:"index:idx_tasks_company_due,priority:2"`
	AssigneeID    *uuid.UUID `gorm:"type:uuid;index"`
	AssigneeName  *string    `gorm:"size:255"`
	CreatedBy     uuid.UUID  `gorm:"type:uuid;not null;index"`
	CreatedByName string     `gorm:"size:255;not null"`
	Tags          []string   `gorm:"serializer:json;type:jsonb"`
	CreatedAt     time.Time  `gorm:"not null;index:idx_tasks_company_created,priority:2"`
	UpdatedAt     time.Time  `gorm:"not null"`
	CompletedAt   *time.Time `gorm:"index"`
}

// Comment — комментарий к задаче.
type Comment struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey"`
	CompanyID  uuid.UUID `gorm:"type:uuid;not null;index"`
	TaskID     uuid.UUID `gorm:"type:uuid;not null;index"`
	AuthorID   uuid.UUID `gorm:"type:uuid;not null"`
	AuthorName string    `gorm:"size:255;not null"`
	Text       string    `gorm:"type:text;not null"`
	CreatedAt  time.Time `gorm:"not null"`
	UpdatedAt  time.Time `gorm:"not null"`
}

// TimeEntry — интервал учёта времени; EndedAt == nil — таймер активен.
type TimeEntry struct {
	ID              uuid.UUID  `gorm:"type:uuid;primaryKey"`
	CompanyID       uuid.UUID  `gorm:"type:uuid;not null;index:idx_time_company_user,priority:1"`
	TaskID          uuid.UUID  `gorm:"type:uuid;not null;index"`
	UserID          uuid.UUID  `gorm:"type:uuid;not null;index:idx_time_company_user,priority:2"`
	StartedAt       time.Time  `gorm:"not null;index"`
	EndedAt         *time.Time `gorm:"index"`
	DurationMinutes int        `gorm:"not null"`
	CreatedAt       time.Time  `gorm:"not null"`
}

// Meeting — встреча.
type Meeting struct {
	ID           uuid.UUID  `gorm:"type:uuid;primaryKey"`
	CompanyID    uuid.UUID  `gorm:"type:uuid;not null;index:idx_meetings_company_start,priority:1"`
	Title        string     `gorm:"size:255;not null"`
	Description  string     `gorm:"type:text;not null"`
	StartAt      time.Time  `gorm:"not null;index:idx_meetings_company_start,priority:2"`
	EndAt        time.Time  `gorm:"not null"`
	Location     string     `gorm:"size:255;not null"`
	Participants []string   `gorm:"serializer:json;type:jsonb"`
	ClientID     *uuid.UUID `gorm:"type:uuid"`
	CreatedBy    uuid.UUID  `gorm:"type:uuid;not null"`
	CreatedAt    time.Time  `gorm:"not null"`
	UpdatedAt    time.Time  `gorm:"not null"`
}

// ImportantDate — важная дата (дедлайн, день рождения и т.п.).
// CreatedBy — автор (получатель напоминаний); у строк, созданных до v2.1 без автора, — uuid.Nil.
type ImportantDate struct {
	ID               uuid.UUID `gorm:"type:uuid;primaryKey"`
	CompanyID        uuid.UUID `gorm:"type:uuid;not null;index:idx_important_dates_company_date,priority:1"`
	Title            string    `gorm:"size:255;not null"`
	Date             time.Time `gorm:"type:date;not null;index:idx_important_dates_company_date,priority:2"`
	Type             string    `gorm:"size:20;not null"`
	RemindDaysBefore int       `gorm:"not null"`
	Description      string    `gorm:"type:text;not null"`
	CreatedBy        uuid.UUID `gorm:"type:uuid;not null"`
	CreatedAt        time.Time `gorm:"not null"`
	UpdatedAt        time.Time `gorm:"not null"`
}

// TaskReminder фиксирует отправленное напоминание о сроке задачи.
// Уникальность (task_id, due_date): при переносе срока напоминание уйдёт снова.
type TaskReminder struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	CompanyID uuid.UUID `gorm:"type:uuid;not null;index"`
	TaskID    uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:uq_task_reminders_task_due,priority:1"`
	DueDate   time.Time `gorm:"not null;uniqueIndex:uq_task_reminders_task_due,priority:2"`
	SentAt    time.Time `gorm:"not null"`
}

// ImportantDateReminder фиксирует отправленное напоминание о важной дате (docs/API.md §12.3).
// Уникальность (important_date_id, occurrence_date): не чаще одного раза на конкретную дату;
// для birthday/holiday occurrence_date — годовщина, поэтому напоминание уходит ежегодно.
type ImportantDateReminder struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey"`
	CompanyID       uuid.UUID `gorm:"type:uuid;not null;index"`
	ImportantDateID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:uq_important_date_reminders_date_occ,priority:1"`
	OccurrenceDate  time.Time `gorm:"type:date;not null;uniqueIndex:uq_important_date_reminders_date_occ,priority:2"`
	SentAt          time.Time `gorm:"not null"`
}

// All — модели для AutoMigrate.
func All() []any {
	return []any{&Task{}, &Comment{}, &TimeEntry{}, &Meeting{}, &ImportantDate{}, &TaskReminder{}, &ImportantDateReminder{}}
}

func ensureID(id *uuid.UUID) {
	if *id == uuid.Nil {
		*id = uuid.New()
	}
}

// BeforeCreate-хуки генерируют UUID и гарантируют не-nil JSON-массивы.

func (t *Task) BeforeCreate(*gorm.DB) error {
	ensureID(&t.ID)
	if t.Tags == nil {
		t.Tags = []string{}
	}
	return nil
}

func (c *Comment) BeforeCreate(*gorm.DB) error { ensureID(&c.ID); return nil }

func (e *TimeEntry) BeforeCreate(*gorm.DB) error { ensureID(&e.ID); return nil }

func (m *Meeting) BeforeCreate(*gorm.DB) error {
	ensureID(&m.ID)
	if m.Participants == nil {
		m.Participants = []string{}
	}
	return nil
}

func (d *ImportantDate) BeforeCreate(*gorm.DB) error { ensureID(&d.ID); return nil }

func (r *TaskReminder) BeforeCreate(*gorm.DB) error { ensureID(&r.ID); return nil }

func (r *ImportantDateReminder) BeforeCreate(*gorm.DB) error { ensureID(&r.ID); return nil }
