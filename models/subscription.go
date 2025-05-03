package models

import (
	"time"

	"github.com/google/uuid"
)

// Subscription представляет подписку компании
type Subscription struct {
	ID        uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	CompanyID uuid.UUID `json:"company_id" gorm:"type:uuid;not null"`
	Status    string    `json:"status" gorm:"not null"` // "active", "inactive"
	ExpiresAt time.Time `json:"expires_at" gorm:"not null"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
