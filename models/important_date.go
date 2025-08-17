package models

import (
	"time"

	"github.com/google/uuid"
)

type ImportantDate struct {
	ID           uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	UserID       string    `json:"user_id" gorm:"not null;index"`
	CompanyID    uuid.UUID `json:"company_id" gorm:"type:uuid;not null;index"`
	Title        string    `json:"title" gorm:"not null"`
	Description  string    `json:"description"`
	Date         time.Time `json:"date" gorm:"not null"`
	Type         string    `json:"type" gorm:"default:other"`      // tax_deadline, contract_expiry, payment_due, birthday, anniversary, other
	Priority     string    `json:"priority" gorm:"default:medium"` // low, medium, high, critical
	ReminderDays []int     `json:"reminder_days" gorm:"type:json"` // За сколько дней напоминать
	ReminderSent bool      `json:"reminder_sent" gorm:"default:false"`
	ExternalID   string    `json:"external_id"` // ID во внешних календарях
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type DateReminder struct {
	ID              uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	ImportantDateID uuid.UUID `json:"important_date_id" gorm:"type:uuid;not null;index"`
	ReminderDate    time.Time `json:"reminder_date" gorm:"not null"`
	Sent            bool      `json:"sent" gorm:"default:false"`
	SentAt          time.Time `json:"sent_at,omitempty"`
	Channel         string    `json:"channel" gorm:"default:email"` // email, sms, telegram, push
	CreatedAt       time.Time `json:"created_at"`
}
