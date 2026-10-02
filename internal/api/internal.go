package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"clerkai/task-service/internal/models"
)

// ServiceTokenHeader — заголовок межсервисной аутентификации (docs/API.md §1).
const ServiceTokenHeader = "X-Service-Token"

// serviceAuth проверяет X-Service-Token для /api/internal/*. Без SERVICE_TOKEN в конфигурации
// внутренний API выключен (503); неверный или отсутствующий токен — 401.
func serviceAuth(expected string) gin.HandlerFunc {
	want := sha256.Sum256([]byte(expected))
	return func(c *gin.Context) {
		if expected == "" {
			fail(c, http.StatusServiceUnavailable, "Внутренний API не настроен")
			return
		}
		// Сравниваем хеши фиксированной длины в постоянное время — не раскрываем ни содержимое, ни длину токена.
		got := sha256.Sum256([]byte(c.GetHeader(ServiceTokenHeader)))
		if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
			fail(c, http.StatusUnauthorized, "Неверный сервисный токен")
			return
		}
		c.Next()
	}
}

type purgeInput struct {
	CompanyID *string  `json:"company_id"`
	UserIDs   []string `json:"user_ids"`
}

// parsePurgeInput проверяет тело {company_id, user_ids}: company_id — обязательный ненулевой UUID,
// user_ids — необязательный список UUID (данные удаляются по company_id: все сущности сервиса
// принадлежат компании, пользователи других компаний здесь не хранятся).
func parsePurgeInput(in purgeInput) (uuid.UUID, string, string) {
	if in.CompanyID == nil || strings.TrimSpace(*in.CompanyID) == "" {
		return uuid.Nil, "company_id", "обязательное поле"
	}
	companyID, err := uuid.Parse(strings.TrimSpace(*in.CompanyID))
	if err != nil || companyID == uuid.Nil {
		return uuid.Nil, "company_id", "ожидается UUID"
	}
	for _, s := range in.UserIDs {
		if _, err := uuid.Parse(strings.TrimSpace(s)); err != nil {
			return uuid.Nil, "user_ids", "ожидается список UUID"
		}
	}
	return companyID, "", ""
}

// purgeModels — порядок удаления: сначала зависимые сущности, затем задачи, встречи и важные даты.
var purgeModels = []any{
	&models.Comment{},
	&models.TimeEntry{},
	&models.TaskReminder{},
	&models.ImportantDateReminder{},
	&models.Task{},
	&models.Meeting{},
	&models.ImportantDate{},
}

// POST /api/internal/tasks/purge (X-Service-Token) — удаление всех данных компании (docs/API.md §12.5).
// Идемпотентно: повторный вызов возвращает {"deleted": 0}.
func (h *Handler) purgeCompany(c *gin.Context) {
	var in purgeInput
	if !bindJSON(c, &in, false) {
		return
	}
	companyID, field, msg := parsePurgeInput(in)
	if field != "" {
		invalid(c, field, msg)
		return
	}
	var deleted int64
	err := h.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		deleted = 0
		for _, m := range purgeModels {
			res := tx.Where("company_id = ?", companyID).Delete(m)
			if res.Error != nil {
				return res.Error
			}
			deleted += res.RowsAffected
		}
		return nil
	})
	if err != nil {
		h.internalError(c, err)
		return
	}
	h.log.Info("данные компании удалены", "company_id", companyID, "users", len(in.UserIDs), "deleted", deleted)
	c.JSON(http.StatusOK, gin.H{"deleted": deleted})
}
