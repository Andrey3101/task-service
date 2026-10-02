package notify

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"clerkai/task-service/internal/models"
)

const dateLayout = "2006-01-02"

// yearlyTypes — типы с ежегодным повтором (см. IsYearly).
var yearlyTypes = []string{"birthday", "holiday"}

type dateCandidate struct {
	d        *models.ImportantDate
	occ      time.Time
	daysLeft int
}

// sendDateReminders — проход по важным датам (docs/API.md §12.3): автору (created_by) уходит
// уведомление, когда до ближайшего наступления даты осталось 0..remind_days_before дней по Москве.
// Не чаще одного раза на (important_date_id, occurrence_date); отметка пишется только после
// успешной отправки — при ошибке повторим на следующем проходе. Строки без автора пропускаются.
func sendDateReminders(ctx context.Context, db *gorm.DB, sender Sender, now time.Time, log *slog.Logger) (int, error) {
	today := TodayMoscow(now)
	todayStr := today.Format(dateLayout)
	// Неповторяющиеся даты вне [today, today+365] заведомо не попадают в окно напоминания.
	maxStr := today.AddDate(0, 0, MaxRemindDaysBefore).Format(dateLayout)

	sent := 0
	lastID := uuid.Nil
	for {
		if err := ctx.Err(); err != nil {
			return sent, err
		}
		var rows []models.ImportantDate
		q := db.WithContext(ctx).
			Where("created_by <> ?", uuid.Nil).
			Where("(type IN ? OR (date >= ? AND date <= ?))", yearlyTypes, todayStr, maxStr)
		if lastID != uuid.Nil {
			q = q.Where("id > ?", lastID)
		}
		if err := q.Order("id ASC").Limit(reminderBatch).Find(&rows).Error; err != nil {
			return sent, err
		}
		if len(rows) == 0 {
			return sent, nil
		}
		lastID = rows[len(rows)-1].ID

		n, err := remindDatesBatch(ctx, db, sender, rows, today, now, log)
		sent += n
		if err != nil {
			return sent, err
		}
		if len(rows) < reminderBatch {
			return sent, nil
		}
	}
}

// remindDatesBatch отбирает из порции даты, по которым пора напомнить, исключает уже отправленные
// и отправляет остальные.
func remindDatesBatch(ctx context.Context, db *gorm.DB, sender Sender, rows []models.ImportantDate,
	today, now time.Time, log *slog.Logger) (int, error) {
	cands := make([]dateCandidate, 0)
	for i := range rows {
		r := &rows[i]
		if r.CreatedBy == uuid.Nil {
			continue
		}
		if occ, days, due := DateReminderDue(r.Date, r.Type, r.RemindDaysBefore, today); due {
			cands = append(cands, dateCandidate{d: r, occ: occ, daysLeft: days})
		}
	}
	if len(cands) == 0 {
		return 0, nil
	}

	ids := make([]uuid.UUID, len(cands))
	for i := range cands {
		ids[i] = cands[i].d.ID
	}
	var done []models.ImportantDateReminder
	err := db.WithContext(ctx).Model(&models.ImportantDateReminder{}).
		Select("important_date_id", "occurrence_date").
		Where("important_date_id IN ? AND occurrence_date >= ?", ids, today.Format(dateLayout)).
		Find(&done).Error
	if err != nil {
		return 0, err
	}
	already := make(map[string]struct{}, len(done))
	for i := range done {
		already[reminderKey(done[i].ImportantDateID, done[i].OccurrenceDate)] = struct{}{}
	}

	sent := 0
	for _, c := range cands {
		if ctx.Err() != nil {
			return sent, ctx.Err()
		}
		if _, ok := already[reminderKey(c.d.ID, c.occ)]; ok {
			continue
		}
		sendCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		err := sender.Send(sendCtx, Notification{
			UserID:    c.d.CreatedBy,
			CompanyID: c.d.CompanyID,
			Title:     "Напоминание: " + c.d.Title,
			Message:   DateReminderMessage(c.occ, c.daysLeft),
			Type:      "info",
			Link:      "/meetings",
		})
		cancel()
		if err != nil {
			// Не записываем отметку — повторим на следующем проходе.
			log.Warn("напоминание о важной дате не отправлено", "important_date_id", c.d.ID, "error", err)
			continue
		}
		rem := models.ImportantDateReminder{
			CompanyID: c.d.CompanyID, ImportantDateID: c.d.ID, OccurrenceDate: c.occ, SentAt: now,
		}
		if err := db.WithContext(ctx).Create(&rem).Error; err != nil {
			log.Warn("не удалось сохранить отметку о напоминании", "important_date_id", c.d.ID, "error", err)
			continue
		}
		sent++
	}
	return sent, nil
}

func reminderKey(id uuid.UUID, occ time.Time) string {
	return id.String() + "|" + civil(occ).Format(dateLayout)
}
