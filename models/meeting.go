package models

import (
	"time"

	"github.com/google/uuid"
)

type Meeting struct {
	ID                 uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	UserID             string    `json:"user_id" gorm:"not null;index"`
	CompanyID          uuid.UUID `json:"company_id" gorm:"type:uuid;not null;index"`
	Title              string    `json:"title" gorm:"not null"`
	Description        string    `json:"description"`
	StartTime          time.Time `json:"start_time" gorm:"not null"`
	EndTime            time.Time `json:"end_time" gorm:"not null"`
	Location           string    `json:"location"`
	MeetingType        string    `json:"meeting_type" gorm:"default:internal"` // internal, client, supplier, partner
	Status             string    `json:"status" gorm:"default:scheduled"`      // scheduled, confirmed, cancelled, completed
	Priority           string    `json:"priority" gorm:"default:medium"`       // low, medium, high, critical
	Participants       []string  `json:"participants" gorm:"type:json"`
	ExternalCalendarID string    `json:"external_calendar_id"` // ID в Google Calendar, Outlook и т.д.
	ReminderSent       bool      `json:"reminder_sent" gorm:"default:false"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type MeetingParticipant struct {
	ID        uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	MeetingID uuid.UUID `json:"meeting_id" gorm:"type:uuid;not null;index"`
	UserID    string    `json:"user_id" gorm:"not null;index"`
	Role      string    `json:"role" gorm:"default:participant"` // organizer, participant, observer
	Status    string    `json:"status" gorm:"default:invited"`   // invited, accepted, declined, tentative
	CreatedAt time.Time `json:"created_at"`
}
