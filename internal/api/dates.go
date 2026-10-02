package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"clerkai/task-service/internal/auth"
	"clerkai/task-service/internal/models"
	"clerkai/task-service/internal/params"
)

const msgDateNotFound = "Важная дата не найдена"

type importantDateInput struct {
	Title            Opt[string] `json:"title"`
	Date             Opt[string] `json:"date"`
	Type             Opt[string] `json:"type"`
	RemindDaysBefore Opt[int]    `json:"remind_days_before"`
	Description      Opt[string] `json:"description"`
}

func applyDateInput(d *models.ImportantDate, in importantDateInput, creating bool) (field, msg string) {
	if creating {
		if !in.Title.Set {
			return "title", "обязательное поле"
		}
		if v, ok := in.Date.Value(); !ok || strings.TrimSpace(v) == "" {
			return "date", "обязательное поле"
		}
	}
	if in.Title.Set {
		title, ok := params.CleanText(in.Title.V, 1, 255)
		if in.Title.Null || !ok {
			return "title", "от 1 до 255 символов"
		}
		d.Title = title
	}
	if in.Date.Set {
		date, err := params.ParseDate(in.Date.V)
		if in.Date.Null || err != nil {
			return "date", "ожидается дата в формате YYYY-MM-DD"
		}
		d.Date = date
	}
	if in.Type.Set {
		if in.Type.Null || !params.OneOf(in.Type.V, params.DateTypes) {
			return "type", "допустимо deadline, birthday, holiday, payment, other"
		}
		d.Type = in.Type.V
	}
	if in.RemindDaysBefore.Set {
		if in.RemindDaysBefore.Null || in.RemindDaysBefore.V < 0 || in.RemindDaysBefore.V > 365 {
			return "remind_days_before", "целое число от 0 до 365"
		}
		d.RemindDaysBefore = in.RemindDaysBefore.V
	}
	if in.Description.Set {
		desc, ok := params.CleanText(in.Description.V, 0, 10000)
		if !ok {
			return "description", "не длиннее 10000 символов"
		}
		d.Description = desc
	}
	return "", ""
}

func (h *Handler) findDate(c *gin.Context, u auth.User) (*models.ImportantDate, bool) {
	id, ok := pathID(c, "id", msgDateNotFound)
	if !ok {
		return nil, false
	}
	var d models.ImportantDate
	err := h.db.WithContext(c.Request.Context()).Where("company_id = ? AND id = ?", u.CompanyID, id).First(&d).Error
	if isNotFound(err) {
		fail(c, http.StatusNotFound, msgDateNotFound)
		return nil, false
	}
	if err != nil {
		h.internalError(c, err)
		return nil, false
	}
	return &d, true
}

// GET /api/important-dates?from&to&page&limit — по возрастанию даты; from/to — YYYY-MM-DD включительно.
func (h *Handler) listDates(c *gin.Context, u auth.User) {
	pg, limit, ok := pagination(c)
	if !ok {
		return
	}
	var from, to string
	for _, p := range []struct {
		key string
		dst *string
	}{{"from", &from}, {"to", &to}} {
		if s := c.Query(p.key); s != "" {
			d, err := params.ParseDate(s)
			if err != nil {
				invalid(c, p.key, err.Error())
				return
			}
			*p.dst = d.Format(params.DateLayout)
		}
	}
	ctx := c.Request.Context()
	query := func() *gorm.DB {
		q := h.db.WithContext(ctx).Model(&models.ImportantDate{}).Where("company_id = ?", u.CompanyID)
		if from != "" {
			q = q.Where("date >= ?", from)
		}
		if to != "" {
			q = q.Where("date <= ?", to)
		}
		return q
	}
	var total int64
	if err := query().Count(&total).Error; err != nil {
		h.internalError(c, err)
		return
	}
	var rows []models.ImportantDate
	if err := query().Order("date ASC, id ASC").Limit(limit).Offset(params.Offset(pg, limit)).Find(&rows).Error; err != nil {
		h.internalError(c, err)
		return
	}
	out := make([]ImportantDateDTO, len(rows))
	for i := range rows {
		out[i] = importantDateDTO(&rows[i])
	}
	c.JSON(http.StatusOK, page[ImportantDateDTO]{Items: out, Total: total, Page: pg, Limit: limit})
}

// POST /api/important-dates
func (h *Handler) createDate(c *gin.Context, u auth.User) {
	var in importantDateInput
	if !bindJSON(c, &in, false) {
		return
	}
	d := models.ImportantDate{CompanyID: u.CompanyID, CreatedBy: u.ID, Type: "other", RemindDaysBefore: 1}
	if field, msg := applyDateInput(&d, in, true); field != "" {
		invalid(c, field, msg)
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Create(&d).Error; err != nil {
		h.internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, importantDateDTO(&d))
}

// GET /api/important-dates/:id
func (h *Handler) getDate(c *gin.Context, u auth.User) {
	if d, ok := h.findDate(c, u); ok {
		c.JSON(http.StatusOK, importantDateDTO(d))
	}
}

// PUT /api/important-dates/:id — частичное обновление.
func (h *Handler) updateDate(c *gin.Context, u auth.User) {
	d, ok := h.findDate(c, u)
	if !ok {
		return
	}
	var in importantDateInput
	if !bindJSON(c, &in, false) {
		return
	}
	if field, msg := applyDateInput(d, in, false); field != "" {
		invalid(c, field, msg)
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Save(d).Error; err != nil {
		h.internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, importantDateDTO(d))
}

// DELETE /api/important-dates/:id
func (h *Handler) deleteDate(c *gin.Context, u auth.User) {
	d, ok := h.findDate(c, u)
	if !ok {
		return
	}
	// Вместе с датой удаляются отметки об отправленных напоминаниях.
	err := h.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("company_id = ? AND important_date_id = ?", u.CompanyID, d.ID).
			Delete(&models.ImportantDateReminder{}).Error; err != nil {
			return err
		}
		return tx.Where("company_id = ? AND id = ?", u.CompanyID, d.ID).Delete(&models.ImportantDate{}).Error
	})
	if err != nil {
		h.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
