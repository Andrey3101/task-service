// models/task.go
package models

import (
	"time"

	"github.com/google/uuid"
)

type Task struct {
	ID          uuid.UUID  `json:"task_id" gorm:"type:uuid;primaryKey"`
	Title       string     `json:"title" gorm:"not null"`
	Description string     `json:"description"`
	Status      string     `json:"status" gorm:"default:pending"`
	DueDate     time.Time  `json:"due_date"`
	CreatedBy   string     `json:"created_by" gorm:"not null"`
	CreatedAt   time.Time  `json:"created_at"`
	Assignees   []Assignee `json:"assignees" gorm:"many2many:task_assignees"`
	Comments    []Comment  `json:"comments" gorm:"foreignKey:TaskID"`
}

type Assignee struct {
	UserID string `json:"user_id" gorm:"primaryKey"`
}
