package notify

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"clerkai/task-service/internal/models"
)

// reminderBatch — максимум задач за один проход.
const reminderBatch = 500

// RunReminders раз в interval ищет задачи со сроком в ближайшие 24 ч без напоминания
// и уведомляет исполнителя (или автора). Останавливается при отмене ctx.
func RunReminders(ctx context.Context, db *gorm.DB, sender Sender, interval time.Duration, log *slog.Logger) {
	// Первый проход — вскоре после старта, затем по тикеру.
	first := time.NewTimer(30 * time.Second)
	defer first.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
		case <-ticker.C:
		}
		n, err := sendDueReminders(ctx, db, sender, time.Now().UTC(), log)
		if err != nil && ctx.Err() == nil {
			log.Error("фоновые напоминания: ошибка", "error", err)
			continue
		}
		if n > 0 {
			log.Info("фоновые напоминания отправлены", "count", n)
		}
	}
}

func sendDueReminders(ctx context.Context, db *gorm.DB, sender Sender, now time.Time, log *slog.Logger) (int, error) {
	var tasks []models.Task
	err := db.WithContext(ctx).
		Where("due_date > ? AND due_date <= ?", now, now.Add(24*time.Hour)).
		Where("status IN ?", []string{"todo", "in_progress"}).
		Where("NOT EXISTS (SELECT 1 FROM task_reminders r WHERE r.task_id = tasks.id AND r.due_date = tasks.due_date)").
		Order("due_date ASC").
		Limit(reminderBatch).
		Find(&tasks).Error
	if err != nil {
		return 0, err
	}
	sent := 0
	for i := range tasks {
		if ctx.Err() != nil {
			return sent, ctx.Err()
		}
		t := &tasks[i]
		recipient := t.CreatedBy
		if t.AssigneeID != nil {
			recipient = *t.AssigneeID
		}
		sendCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		err := sender.Send(sendCtx, Notification{
			UserID:    recipient,
			CompanyID: t.CompanyID,
			Title:     "Срок задачи истекает в течение суток",
			Message:   fmt.Sprintf("«%s» — срок %s UTC", t.Title, t.DueDate.UTC().Format("02.01.2006 15:04")),
			Type:      "task",
			Link:      "/tasks/" + t.ID.String(),
		})
		cancel()
		if err != nil {
			// Не записываем напоминание — повторим на следующем проходе.
			log.Warn("напоминание о сроке не отправлено", "task_id", t.ID, "error", err)
			continue
		}
		rem := models.TaskReminder{CompanyID: t.CompanyID, TaskID: t.ID, DueDate: *t.DueDate, SentAt: now}
		if err := db.WithContext(ctx).Create(&rem).Error; err != nil {
			log.Warn("не удалось сохранить отметку о напоминании", "task_id", t.ID, "error", err)
			continue
		}
		sent++
	}
	return sent, nil
}
