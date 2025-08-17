package models

import (
	"time"

	"github.com/google/uuid"
)

type ExternalIntegration struct {
	ID           uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	UserID       string    `json:"user_id" gorm:"not null;index"`
	CompanyID    uuid.UUID `json:"company_id" gorm:"type:uuid;not null;index"`
	Provider     string    `json:"provider" gorm:"not null"`     // google_calendar, outlook, slack, telegram, whatsapp
	Type         string    `json:"type" gorm:"not null"`         // calendar, messaging, notification, crm
	Credentials  string    `json:"credentials" gorm:"type:json"` // Зашифрованные учетные данные
	IsActive     bool      `json:"is_active" gorm:"default:true"`
	LastSync     time.Time `json:"last_sync"`
	SyncInterval int       `json:"sync_interval" gorm:"default:60"` // в минутах
	Settings     string    `json:"settings" gorm:"type:json"`       // Дополнительные настройки
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type IntegrationLog struct {
	ID            uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	IntegrationID uuid.UUID `json:"integration_id" gorm:"type:uuid;not null;index"`
	Action        string    `json:"action" gorm:"not null"` // sync, auth, error, success
	Status        string    `json:"status" gorm:"not null"` // success, error, warning
	Message       string    `json:"message"`
	Details       string    `json:"details" gorm:"type:json"`
	Duration      int       `json:"duration"` // в миллисекундах
	CreatedAt     time.Time `json:"created_at"`
}
