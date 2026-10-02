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

const msgMeetingNotFound = "Встреча не найдена"

type meetingInput struct {
	Title        Opt[string]   `json:"title"`
	Description  Opt[string]   `json:"description"`
	StartAt      Opt[string]   `json:"start_at"`
	EndAt        Opt[string]   `json:"end_at"`
	Location     Opt[string]   `json:"location"`
	Participants Opt[[]string] `json:"participants"`
	ClientID     Opt[string]   `json:"client_id"`
}

func applyMeetingInput(m *models.Meeting, in meetingInput, creating bool) (field, msg string) {
	if creating {
		required := []struct {
			name string
			opt  Opt[string]
		}{{"title", in.Title}, {"start_at", in.StartAt}, {"end_at", in.EndAt}}
		for _, r := range required {
			if v, ok := r.opt.Value(); !ok || strings.TrimSpace(v) == "" {
				return r.name, "обязательное поле"
			}
		}
	}
	if in.Title.Set {
		title, ok := params.CleanText(in.Title.V, 1, 255)
		if in.Title.Null || !ok {
			return "title", "от 1 до 255 символов"
		}
		m.Title = title
	}
	if in.Description.Set {
		desc, ok := params.CleanText(in.Description.V, 0, 10000)
		if !ok {
			return "description", "не длиннее 10000 символов"
		}
		m.Description = desc
	}
	if in.StartAt.Set {
		t, err := params.ParseDateTime(in.StartAt.V)
		if in.StartAt.Null || err != nil {
			return "start_at", "ожидается дата-время ISO 8601"
		}
		m.StartAt = t
	}
	if in.EndAt.Set {
		t, err := params.ParseDateTime(in.EndAt.V)
		if in.EndAt.Null || err != nil {
			return "end_at", "ожидается дата-время ISO 8601"
		}
		m.EndAt = t
	}
	if m.EndAt.Before(m.StartAt) {
		return "end_at", "не может быть раньше start_at"
	}
	if in.Location.Set {
		loc, ok := params.CleanText(in.Location.V, 0, 255)
		if !ok {
			return "location", "не длиннее 255 символов"
		}
		m.Location = loc
	}
	if in.Participants.Set {
		list, ok := params.CleanList(in.Participants.V, 100, 255)
		if !ok {
			return "participants", "не более 100 участников, до 255 символов каждый"
		}
		m.Participants = list
	}
	if in.ClientID.Set {
		id, err := optUUID(in.ClientID)
		if err != nil {
			return "client_id", err.Error()
		}
		m.ClientID = id
	}
	return "", ""
}

func (h *Handler) findMeeting(c *gin.Context, u auth.User) (*models.Meeting, bool) {
	id, ok := pathID(c, "id", msgMeetingNotFound)
	if !ok {
		return nil, false
	}
	var m models.Meeting
	err := h.db.WithContext(c.Request.Context()).Where("company_id = ? AND id = ?", u.CompanyID, id).First(&m).Error
	if isNotFound(err) {
		fail(c, http.StatusNotFound, msgMeetingNotFound)
		return nil, false
	}
	if err != nil {
		h.internalError(c, err)
		return nil, false
	}
	return &m, true
}

// GET /api/meetings?from&to&page&limit — встречи, начинающиеся в [from, to), по возрастанию начала.
func (h *Handler) listMeetings(c *gin.Context, u auth.User) {
	pg, limit, ok := pagination(c)
	if !ok {
		return
	}
	from, to, ok := queryRange(c, "from", "to")
	if !ok {
		return
	}
	ctx := c.Request.Context()
	query := func() *gorm.DB {
		q := h.db.WithContext(ctx).Model(&models.Meeting{}).Where("company_id = ?", u.CompanyID)
		if from != nil {
			q = q.Where("start_at >= ?", *from)
		}
		if to != nil {
			q = q.Where("start_at < ?", *to)
		}
		return q
	}
	var total int64
	if err := query().Count(&total).Error; err != nil {
		h.internalError(c, err)
		return
	}
	var rows []models.Meeting
	if err := query().Order("start_at ASC, id ASC").Limit(limit).Offset(params.Offset(pg, limit)).Find(&rows).Error; err != nil {
		h.internalError(c, err)
		return
	}
	out := make([]MeetingDTO, len(rows))
	for i := range rows {
		out[i] = meetingDTO(&rows[i])
	}
	c.JSON(http.StatusOK, page[MeetingDTO]{Items: out, Total: total, Page: pg, Limit: limit})
}

// POST /api/meetings
func (h *Handler) createMeeting(c *gin.Context, u auth.User) {
	var in meetingInput
	if !bindJSON(c, &in, false) {
		return
	}
	m := models.Meeting{CompanyID: u.CompanyID, CreatedBy: u.ID, Participants: []string{}}
	if field, msg := applyMeetingInput(&m, in, true); field != "" {
		invalid(c, field, msg)
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Create(&m).Error; err != nil {
		h.internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, meetingDTO(&m))
}

// GET /api/meetings/:id
func (h *Handler) getMeeting(c *gin.Context, u auth.User) {
	if m, ok := h.findMeeting(c, u); ok {
		c.JSON(http.StatusOK, meetingDTO(m))
	}
}

// PUT /api/meetings/:id — частичное обновление.
func (h *Handler) updateMeeting(c *gin.Context, u auth.User) {
	m, ok := h.findMeeting(c, u)
	if !ok {
		return
	}
	var in meetingInput
	if !bindJSON(c, &in, false) {
		return
	}
	if field, msg := applyMeetingInput(m, in, false); field != "" {
		invalid(c, field, msg)
		return
	}
	if m.Participants == nil {
		m.Participants = []string{}
	}
	if err := h.db.WithContext(c.Request.Context()).Save(m).Error; err != nil {
		h.internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, meetingDTO(m))
}

// DELETE /api/meetings/:id
func (h *Handler) deleteMeeting(c *gin.Context, u auth.User) {
	m, ok := h.findMeeting(c, u)
	if !ok {
		return
	}
	if err := h.db.WithContext(c.Request.Context()).
		Where("company_id = ? AND id = ?", u.CompanyID, m.ID).Delete(&models.Meeting{}).Error; err != nil {
		h.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
