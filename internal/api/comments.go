package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"clerkai/task-service/internal/auth"
	"clerkai/task-service/internal/models"
	"clerkai/task-service/internal/params"
)

const msgCommentNotFound = "Комментарий не найден"

type commentInput struct {
	Text Opt[string] `json:"text"`
}

func commentText(c *gin.Context, in commentInput) (string, bool) {
	text, ok := params.CleanText(in.Text.V, 1, 5000)
	if !in.Text.Set || in.Text.Null || !ok {
		invalid(c, "text", "от 1 до 5000 символов")
		return "", false
	}
	return text, true
}

// GET /api/tasks/:id/comments — по возрастанию времени.
func (h *Handler) listComments(c *gin.Context, u auth.User) {
	t, ok := h.findTask(c, u)
	if !ok {
		return
	}
	pg, limit, ok := pagination(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	var total int64
	if err := h.db.WithContext(ctx).Model(&models.Comment{}).
		Where("company_id = ? AND task_id = ?", u.CompanyID, t.ID).Count(&total).Error; err != nil {
		h.internalError(c, err)
		return
	}
	var rows []models.Comment
	if err := h.db.WithContext(ctx).
		Where("company_id = ? AND task_id = ?", u.CompanyID, t.ID).
		Order("created_at ASC, id ASC").Limit(limit).Offset(params.Offset(pg, limit)).
		Find(&rows).Error; err != nil {
		h.internalError(c, err)
		return
	}
	out := make([]CommentDTO, len(rows))
	for i := range rows {
		out[i] = commentDTO(&rows[i])
	}
	c.JSON(http.StatusOK, page[CommentDTO]{Items: out, Total: total, Page: pg, Limit: limit})
}

// POST /api/tasks/:id/comments
func (h *Handler) createComment(c *gin.Context, u auth.User) {
	t, ok := h.findTask(c, u)
	if !ok {
		return
	}
	var in commentInput
	if !bindJSON(c, &in, false) {
		return
	}
	text, ok := commentText(c, in)
	if !ok {
		return
	}
	m := models.Comment{CompanyID: u.CompanyID, TaskID: t.ID, AuthorID: u.ID, AuthorName: u.Name, Text: text}
	if err := h.db.WithContext(c.Request.Context()).Create(&m).Error; err != nil {
		h.internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, commentDTO(&m))
}

// findComment — комментарий задачи :id в компании; изменять может автор или owner/admin.
func (h *Handler) findComment(c *gin.Context, u auth.User) (*models.Comment, bool) {
	t, ok := h.findTask(c, u)
	if !ok {
		return nil, false
	}
	cid, ok := pathID(c, "comment_id", msgCommentNotFound)
	if !ok {
		return nil, false
	}
	var m models.Comment
	err := h.db.WithContext(c.Request.Context()).
		Where("company_id = ? AND task_id = ? AND id = ?", u.CompanyID, t.ID, cid).First(&m).Error
	if isNotFound(err) {
		fail(c, http.StatusNotFound, msgCommentNotFound)
		return nil, false
	}
	if err != nil {
		h.internalError(c, err)
		return nil, false
	}
	if m.AuthorID != u.ID && !u.IsAdmin() {
		fail(c, http.StatusForbidden, msgForbidden)
		return nil, false
	}
	return &m, true
}

// PUT /api/tasks/:id/comments/:comment_id
func (h *Handler) updateComment(c *gin.Context, u auth.User) {
	m, ok := h.findComment(c, u)
	if !ok {
		return
	}
	var in commentInput
	if !bindJSON(c, &in, false) {
		return
	}
	text, ok := commentText(c, in)
	if !ok {
		return
	}
	m.Text = text
	if err := h.db.WithContext(c.Request.Context()).Save(m).Error; err != nil {
		h.internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, commentDTO(m))
}

// DELETE /api/tasks/:id/comments/:comment_id
func (h *Handler) deleteComment(c *gin.Context, u auth.User) {
	m, ok := h.findComment(c, u)
	if !ok {
		return
	}
	if err := h.db.WithContext(c.Request.Context()).
		Where("company_id = ? AND id = ?", u.CompanyID, m.ID).Delete(&models.Comment{}).Error; err != nil {
		h.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
