package models

import (
	"time"

	"github.com/google/uuid"
)

// CalendarEvent представляет событие в календаре
type CalendarEvent struct {
	ID           uuid.UUID            `json:"id" gorm:"type:uuid;primaryKey"`
	UserID       string               `json:"user_id" gorm:"not null;index"`
	CompanyID    uuid.UUID            `json:"company_id" gorm:"type:uuid;not null;index"`
	Title        string               `json:"title" gorm:"not null"`
	Description  string               `json:"description"`
	StartTime    time.Time            `json:"start_time" gorm:"not null;index"`
	EndTime      time.Time            `json:"end_time" gorm:"not null"`
	Location     string               `json:"location"`
	EventType    string               `json:"event_type" gorm:"not null;default:'custom'"` // custom, task, meeting, reminder
	Priority     string               `json:"priority" gorm:"default:'medium'"`            // low, medium, high, urgent
	Status       string               `json:"status" gorm:"default:'scheduled'"`           // scheduled, in_progress, completed, cancelled
	Color        string               `json:"color" gorm:"default:'#3B82F6'"`              // HEX цвет для UI
	IsAllDay     bool                 `json:"is_all_day" gorm:"default:false"`
	Recurring    string               `json:"recurring"` // none, daily, weekly, monthly, yearly
	RecurringEnd *time.Time           `json:"recurring_end"`
	Reminders    []CalendarReminder   `json:"reminders" gorm:"foreignKey:EventID"`
	Attachments  []CalendarAttachment `json:"attachments" gorm:"foreignKey:EventID"`
	Tags         []CalendarTag        `json:"tags" gorm:"many2many:calendar_event_tags;"`
	CreatedAt    time.Time            `json:"created_at"`
	UpdatedAt    time.Time            `json:"updated_at"`
}

// CalendarReminder представляет напоминание о событии
type CalendarReminder struct {
	ID        uuid.UUID  `json:"id" gorm:"type:uuid;primaryKey"`
	EventID   uuid.UUID  `json:"event_id" gorm:"type:uuid;not null;index"`
	Type      string     `json:"type" gorm:"not null"`    // email, push, sms
	Minutes   int        `json:"minutes" gorm:"not null"` // за сколько минут до события
	IsSent    bool       `json:"is_sent" gorm:"default:false"`
	SentAt    *time.Time `json:"sent_at"`
	CreatedAt time.Time  `json:"created_at"`
}

// CalendarAttachment представляет вложение к событию
type CalendarAttachment struct {
	ID         uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	EventID    uuid.UUID `json:"event_id" gorm:"type:uuid;not null;index"`
	FileName   string    `json:"file_name" gorm:"not null"`
	FileSize   int64     `json:"file_size"`
	FileType   string    `json:"file_type"`
	FilePath   string    `json:"file_path"`
	UploadedBy string    `json:"uploaded_by"`
	CreatedAt  time.Time `json:"created_at"`
}

// CalendarTag представляет тег для событий
type CalendarTag struct {
	ID        uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	Name      string    `json:"name" gorm:"not null;uniqueIndex"`
	Color     string    `json:"color" gorm:"default:'#6B7280'"`
	CreatedAt time.Time `json:"created_at"`
}

// CalendarSettings представляет настройки календаря пользователя
type CalendarSettings struct {
	ID                uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	UserID            string    `json:"user_id" gorm:"not null;uniqueIndex"`
	DefaultView       string    `json:"default_view" gorm:"default:'month'"` // day, week, month, year
	WorkingHoursStart string    `json:"working_hours_start" gorm:"default:'09:00'"`
	WorkingHoursEnd   string    `json:"working_hours_end" gorm:"default:'18:00'"`
	WeekStart         string    `json:"week_start" gorm:"default:'monday'"` // monday, sunday
	TimeZone          string    `json:"time_zone" gorm:"default:'UTC'"`
	ShowWeekends      bool      `json:"show_weekends" gorm:"default:true"`
	ShowHolidays      bool      `json:"show_holidays" gorm:"default:true"`
	AutoSyncTasks     bool      `json:"auto_sync_tasks" gorm:"default:true"`
	AutoSyncMeetings  bool      `json:"auto_sync_meetings" gorm:"default:true"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// CalendarHoliday представляет праздничные дни
type CalendarHoliday struct {
	ID          uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	Date        time.Time `json:"date" gorm:"not null;uniqueIndex"`
	Name        string    `json:"name" gorm:"not null"`
	Description string    `json:"description"`
	IsRecurring bool      `json:"is_recurring" gorm:"default:true"`
	Country     string    `json:"country" gorm:"default:'RU'"`
	CreatedAt   time.Time `json:"created_at"`
}

// CalendarSyncLog представляет лог синхронизации календаря
type CalendarSyncLog struct {
	ID         uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	UserID     string    `json:"user_id" gorm:"not null;index"`
	Action     string    `json:"action" gorm:"not null"` // sync_tasks, sync_meetings, manual_sync
	Status     string    `json:"status" gorm:"not null"` // success, error, partial
	ItemsCount int       `json:"items_count"`
	Error      string    `json:"error"`
	Duration   int       `json:"duration"` // в миллисекундах
	CreatedAt  time.Time `json:"created_at"`
}
