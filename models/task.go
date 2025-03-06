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

type Assignee struct {
	UserID string `json:"user_id" gorm:"primaryKey"`
}
