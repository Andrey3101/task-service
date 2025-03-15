// models/comment.go
package models

import (
	"time"

	"github.com/google/uuid"
)

type Comment struct {
	ID        uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	TaskID    uuid.UUID `json:"task_id" gorm:"not null"`
	UserID    string    `json:"user_id" gorm:"not null"`
	Text      string    `json:"text" gorm:"not null"`
	CreatedAt time.Time `json:"created_at"`
}
