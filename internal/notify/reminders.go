package notify

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"clerkai/task-service/internal/models"
)

// reminderBatch — максимум задач (важных дат — за одну порцию) за один проход.
const reminderBatch = 500

// firstRunDelay — задержка первого прохода после старта сервиса.
const firstRunDelay = 30 * time.Second

// RunReminders — фоновая задача напоминаний: первый проход через 30 с после старта, далее
// раз в interval (env REMINDERS_INTERVAL_SEC). За проход: сроки задач в ближайшие 24 ч
// (исполнителю или автору) и важные даты (автору, docs/API.md §12.3). Останавливается при отмене ctx.
func RunReminders(ctx context.Context, db *gorm.DB, sender Sender, interval time.Duration, log *slog.Logger) {
	if interval <= 0 {
		interval = time.Hour
	}
	timer := time.NewTimer(firstRunDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		now := time.Now().UTC()
		runStep(ctx, log, "task_due", func() (int, error) { return sendDueReminders(ctx, db, sender, now, log) })
		runStep(ctx, log, "important_dates", func() (int, error) { return sendDateReminders(ctx, db, sender, now, log) })
		// Следующий проход — через interval после окончания текущего (проходы не накладываются).
		timer.Reset(interval)
	}
}

// runStep выполняет один шаг прохода; ошибка или panic шага логируются и не останавливают цикл.
func runStep(ctx context.Context, log *slog.Logger, step string, fn func() (int, error)) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Error("фоновые напоминания: panic", "step", step, "panic", fmt.Sprint(rec))
		}
	}()
	n, err := fn()
	if err != nil && ctx.Err() == nil {
		log.Error("фоновые напоминания: ошибка", "step", step, "error", err)
	}
	if n > 0 {
		log.Info("фоновые напоминания отправлены", "step", step, "count", n)
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
