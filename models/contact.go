package models

import (
	"time"

	"github.com/google/uuid"
)

type Contact struct {
	ID                     uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	UserID                 string    `json:"user_id" gorm:"not null;index"`
	CompanyID              uuid.UUID `json:"company_id" gorm:"type:uuid;not null;index"`
	Name                   string    `json:"name" gorm:"not null"`
	Type                   string    `json:"type" gorm:"default:client"` // client, supplier, partner, employee, prospect
	Email                  string    `json:"email"`
	Phone                  string    `json:"phone"`
	Company                string    `json:"company"`
	Position               string    `json:"position"`
	PreferredContactMethod string    `json:"preferred_contact_method" gorm:"default:email"` // email, phone, sms, telegram, whatsapp
	Timezone               string    `json:"timezone"`
	WorkingHours           string    `json:"working_hours" gorm:"type:json"` // JSON с рабочими часами
	Notes                  string    `json:"notes"`
	Tags                   []string  `json:"tags" gorm:"type:json"`
	Source                 string    `json:"source"`                       // manual, import, website, referral
	Status                 string    `json:"status" gorm:"default:active"` // active, inactive, archived
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type ContactInteraction struct {
	ID             uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	ContactID      uuid.UUID `json:"contact_id" gorm:"type:uuid;not null;index"`
	UserID         string    `json:"user_id" gorm:"not null;index"`
	Type           string    `json:"type" gorm:"not null"`              // call, email, meeting, message
	Direction      string    `json:"direction" gorm:"default:outbound"` // inbound, outbound
	Subject        string    `json:"subject"`
	Content        string    `json:"content"`
	Duration       int       `json:"duration"`                        // в минутах для звонков
	Status         string    `json:"status" gorm:"default:completed"` // scheduled, completed, cancelled, failed
	NextAction     string    `json:"next_action"`
	NextActionDate time.Time `json:"next_action_date"`
	CreatedAt      time.Time `json:"created_at"`
}
