package models

import (
	"time"

	"github.com/google/uuid"
)

type Task struct {
	ID          uuid.UUID  `json:"id" gorm:"type:uuid;primaryKey"`
	Title       string     `json:"title" gorm:"not null"`
	Description string     `json:"description"`
	Status      string     `json:"status" gorm:"default:pending"`
	DueDate     time.Time  `json:"due_date"`
	CreatedBy   string     `json:"created_by" gorm:"not null"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt time.Time  `json:"completed_at,omitempty"`
	Assignees   []Assignee `json:"assignees" gorm:"many2many:task_assignees"`
	AssigneeIDs []string   `json:"assignee_ids"`
	Comments    []Comment  `json:"comments" gorm:"foreignKey:TaskID"`
	Priority    int        `json:"priority" gorm:"type:integer"`
	Cost        float64    `json:"cost"`
	Stage       string     `json:"stage" gorm:"default:lead"`
	Channel     string     `json:"channel"`
	CompanyID   uuid.UUID  `json:"company_id" gorm:"type:uuid;not null"`
}

type FinancialEvent struct {
	EventID   uuid.UUID `json:"event_id" gorm:"type:uuid;primaryKey"`
	UserID    string    `json:"user_id" gorm:"not null"`
	Title     string    `json:"title" gorm:"not null"`
	Amount    float64   `json:"amount" gorm:"type:decimal(10,2)"`
	DueDate   time.Time `json:"due_date" gorm:"not null"`
	Status    string    `json:"status" gorm:"default:pending"`
	Category  string    `json:"category" gorm:"default:other"`
	CreatedAt time.Time `json:"created_at"`
	CompanyID uuid.UUID `json:"company_id" gorm:"type:uuid"`
}

type TimeEntry struct {
	ID        uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	UserID    string    `json:"user_id" gorm:"not null"`
	TaskID    uuid.UUID `json:"task_id" gorm:"type:uuid"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time,omitempty"`
	Duration  int       `json:"duration"` // В минутах
}

type Assignee struct {
	UserID string    `json:"user_id" gorm:"primaryKey"`
	TaskID uuid.UUID `json:"task_id" gorm:"type:uuid;primaryKey"`
}

type Client struct {
	ClientID  uuid.UUID `json:"client_id" gorm:"type:uuid;primaryKey"`
	CompanyID uuid.UUID `json:"company_id" gorm:"type:uuid;not null"`
	Name      string    `json:"name" gorm:"not null"`
	Phone     string    `json:"phone"`
	Email     string    `json:"email"`
}

type Payroll struct {
	ID         uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	UserID     string    `json:"user_id" gorm:"not null"`
	Period     string    `json:"period"`
	Hours      int       `json:"hours"`
	Rate       float64   `json:"rate"`
	TaxRate    float64   `json:"tax_rate"`
	NetSalary  float64   `json:"net_salary"`
	Calculated time.Time `json:"calculated_at"`
	CompanyID  uuid.UUID `json:"company_id" gorm:"type:uuid;not null"`
}

type OnboardingChecklist struct {
	ID        uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	UserID    string    `json:"user_id" gorm:"not null"`
	TaskTitle string    `json:"task_title"`
	Completed bool      `json:"completed"`
	DueDate   time.Time `json:"due_date"`
}

type LeaveRequest struct {
	ID        uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	UserID    string    `json:"user_id" gorm:"not null"`
	Type      string    `json:"type"` // "vacation" или "sick"
	StartDate time.Time `json:"start_date"`
	EndDate   time.Time `json:"end_date"`
	Status    string    `json:"status" gorm:"default:pending"`
	CompanyID uuid.UUID `json:"company_id" gorm:"type:uuid;not null"`
}
