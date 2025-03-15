package models

import (
	"time"

	"github.com/google/uuid"
)

type Task struct {
	ID          uuid.UUID  `json:"task_id" gorm:"type:uuid;primaryKey;column:task_id"`
	Title       string     `json:"title" gorm:"not null"`
	Description string     `json:"description"`
	Status      string     `json:"status" gorm:"default:pending"`
	DueDate     time.Time  `json:"due_date"`
	CreatedBy   string     `json:"created_by" gorm:"not null"`
	CreatedAt   time.Time  `json:"created_at"`
	Assignees   []Assignee `json:"assignees" gorm:"many2many:task_assignees"`
	Comments    []Comment  `json:"comments" gorm:"foreignKey:TaskID"`
	AssigneeIDs []string   `gorm:"type:text[]" json:"assignee_ids,omitempty"`
	Priority    int        `json:"priority" gorm:"type:integer"`
	Cost        float64    `json:"cost"`
}

type FinancialEvent struct {
	EventID   uuid.UUID `json:"event_id" gorm:"type:uuid;primaryKey"`
	UserID    string    `json:"user_id" gorm:"not null"`
	Title     string    `json:"title" gorm:"not null"`
	Amount    float64   `json:"amount" gorm:"type:decimal(10,2)"`
	DueDate   time.Time `json:"due_date" gorm:"not null"`
	Status    string    `json:"status" gorm:"default:pending"`
	Category  string    `json:"category" gorm:"default:other"` // "taxes", "salary", "bills", "other"
	CreatedAt time.Time `json:"created_at"`
}

type TimeEntry struct {
	EntryID   uuid.UUID  `json:"entry_id" gorm:"type:uuid;primaryKey"`
	TaskID    uuid.UUID  `json:"task_id" gorm:"not null"`
	UserID    string     `json:"user_id" gorm:"not null"`
	StartTime time.Time  `json:"start_time" gorm:"not null"`
	EndTime   *time.Time `json:"end_time"`
	Duration  int64      `json:"duration" gorm:"default:0"` // В секундах
}

type Assignee struct {
	UserID string `json:"user_id" gorm:"primaryKey"`
}
