package api

import (
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"clerkai/task-service/internal/auth"
	"clerkai/task-service/internal/models"
	"clerkai/task-service/internal/params"
)

// maxReportDays — максимальная длина периода отчёта по времени.
const maxReportDays = 366

// stopEntries закрывает активные таймеры, выставляя ended_at и duration_minutes.
func stopEntries(tx *gorm.DB, entries []models.TimeEntry, now time.Time) error {
	for i := range entries {
		end := now
		entries[i].EndedAt = &end
		entries[i].DurationMinutes = params.DurationMinutes(entries[i].StartedAt, now)
		if err := tx.Save(&entries[i]).Error; err != nil {
			return err
		}
	}
	return nil
}

func activeEntriesQuery(tx *gorm.DB, u auth.User) *gorm.DB {
	return tx.Where("company_id = ? AND user_id = ? AND ended_at IS NULL", u.CompanyID, u.ID)
}

// POST /api/tasks/:id/time/start — предыдущий активный таймер пользователя останавливается.
func (h *Handler) startTimer(c *gin.Context, u auth.User) {
	t, ok := h.findTask(c, u)
	if !ok {
		return
	}
	now := h.now()
	entry := models.TimeEntry{CompanyID: u.CompanyID, TaskID: t.ID, UserID: u.ID, StartedAt: now}
	err := h.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var active []models.TimeEntry
		if err := activeEntriesQuery(tx, u).Find(&active).Error; err != nil {
			return err
		}
		if err := stopEntries(tx, active, now); err != nil {
			return err
		}
		return tx.Create(&entry).Error
	})
	if err != nil {
		h.internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, timeEntryDTO(&entry, t.Title, now))
}

type stopInput struct {
	TaskID Opt[string] `json:"task_id"`
}

// POST /api/time/stop — {task_id?}; нет активного таймера → 404.
func (h *Handler) stopTimer(c *gin.Context, u auth.User) {
	var in stopInput
	if !bindJSON(c, &in, true) {
		return
	}
	taskID, err := optUUID(in.TaskID)
	if err != nil {
		invalid(c, "task_id", err.Error())
		return
	}
	now := h.now()
	ctx := c.Request.Context()
	var stopped []models.TimeEntry
	err = h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := activeEntriesQuery(tx, u)
		if taskID != nil {
			q = q.Where("task_id = ?", *taskID)
		}
		if err := q.Order("started_at DESC").Find(&stopped).Error; err != nil {
			return err
		}
		return stopEntries(tx, stopped, now)
	})
	if err != nil {
		h.internalError(c, err)
		return
	}
	if len(stopped) == 0 {
		fail(c, http.StatusNotFound, "Нет активного таймера")
		return
	}
	latest := &stopped[0]
	titles, err := h.taskTitles(ctx, u.CompanyID, []uuid.UUID{latest.TaskID})
	if err != nil {
		h.internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, timeEntryDTO(latest, titles[latest.TaskID], now))
}

func (h *Handler) entryDTOs(c *gin.Context, u auth.User, entries []models.TimeEntry, now time.Time) ([]TimeEntryDTO, bool) {
	idSet := make(map[uuid.UUID]struct{}, len(entries))
	ids := make([]uuid.UUID, 0, len(entries))
	for i := range entries {
		if _, ok := idSet[entries[i].TaskID]; !ok {
			idSet[entries[i].TaskID] = struct{}{}
			ids = append(ids, entries[i].TaskID)
		}
	}
	titles, err := h.taskTitles(c.Request.Context(), u.CompanyID, ids)
	if err != nil {
		h.internalError(c, err)
		return nil, false
	}
	out := make([]TimeEntryDTO, len(entries))
	for i := range entries {
		out[i] = timeEntryDTO(&entries[i], titles[entries[i].TaskID], now)
	}
	return out, true
}

// GET /api/time/active — активные таймеры текущего пользователя.
func (h *Handler) activeTimers(c *gin.Context, u auth.User) {
	var entries []models.TimeEntry
	if err := activeEntriesQuery(h.db.WithContext(c.Request.Context()), u).
		Order("started_at DESC").Find(&entries).Error; err != nil {
		h.internalError(c, err)
		return
	}
	out, ok := h.entryDTOs(c, u, entries, h.now())
	if !ok {
		return
	}
	c.JSON(http.StatusOK, items[TimeEntryDTO]{Items: out})
}

type taskMinutes struct {
	TaskID  uuid.UUID `json:"task_id"`
	Title   string    `json:"title"`
	Minutes int64     `json:"minutes"`
}

type timeReport struct {
	TotalMinutes int64          `json:"total_minutes"`
	ByTask       []taskMinutes  `json:"by_task"`
	Entries      []TimeEntryDTO `json:"entries"`
}

// summarize считает итоги по задачам (по убыванию минут).
func summarize(entries []TimeEntryDTO) timeReport {
	rep := timeReport{ByTask: []taskMinutes{}, Entries: entries}
	idx := make(map[uuid.UUID]int)
	for _, e := range entries {
		rep.TotalMinutes += int64(e.DurationMinutes)
		i, ok := idx[e.TaskID]
		if !ok {
			i = len(rep.ByTask)
			idx[e.TaskID] = i
			rep.ByTask = append(rep.ByTask, taskMinutes{TaskID: e.TaskID, Title: e.TaskTitle})
		}
		rep.ByTask[i].Minutes += int64(e.DurationMinutes)
	}
	sort.SliceStable(rep.ByTask, func(a, b int) bool { return rep.ByTask[a].Minutes > rep.ByTask[b].Minutes })
	return rep
}

// GET /api/time/report?from&to&user_id — интервалы, начатые в [from, to).
// По умолчанию — последние 30 дней. user видит только себя; owner/admin — любого,
// а без user_id — всю компанию.
func (h *Handler) timeReport(c *gin.Context, u auth.User) {
	from, to, ok := queryRange(c, "from", "to")
	if !ok {
		return
	}
	now := h.now()
	if to == nil {
		end := now
		to = &end
	}
	if from == nil {
		start := startOfDayUTC(to.AddDate(0, 0, -30))
		from = &start
	}
	if !to.After(*from) {
		fail(c, http.StatusUnprocessableEntity, "Параметр to должен быть позже from")
		return
	}
	if to.Sub(*from) > maxReportDays*24*time.Hour {
		fail(c, http.StatusUnprocessableEntity, "Период отчёта — не более 366 дней")
		return
	}

	var userFilter *uuid.UUID
	if s := c.Query("user_id"); s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			invalid(c, "user_id", "ожидается UUID")
			return
		}
		if id != u.ID && !u.IsAdmin() {
			fail(c, http.StatusForbidden, msgForbidden)
			return
		}
		userFilter = &id
	} else if !u.IsAdmin() {
		userFilter = &u.ID
	}

	q := h.db.WithContext(c.Request.Context()).
		Where("company_id = ? AND started_at >= ? AND started_at < ?", u.CompanyID, *from, *to)
	if userFilter != nil {
		q = q.Where("user_id = ?", *userFilter)
	}
	var entries []models.TimeEntry
	if err := q.Order("started_at ASC").Find(&entries).Error; err != nil {
		h.internalError(c, err)
		return
	}
	out, ok := h.entryDTOs(c, u, entries, now)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, summarize(out))
}
