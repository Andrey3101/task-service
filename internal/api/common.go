// Package api — HTTP-слой task-service (маршруты /api/tasks, /api/time, /api/meetings, /api/important-dates).
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"clerkai/task-service/internal/auth"
	"clerkai/task-service/internal/notify"
	"clerkai/task-service/internal/params"
)

// Handler — зависимости обработчиков.
type Handler struct {
	db       *gorm.DB
	notifier notify.Sender
	log      *slog.Logger
	now      func() time.Time
}

// NewHandler создаёт обработчики.
func NewHandler(db *gorm.DB, notifier notify.Sender, log *slog.Logger) *Handler {
	return &Handler{db: db, notifier: notifier, log: log, now: func() time.Time { return time.Now().UTC() }}
}

type userHandler func(c *gin.Context, u auth.User)

// wrap достаёт пользователя из контекста (его кладёт auth.Middleware).
func (h *Handler) wrap(fn userHandler) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, ok := auth.CurrentUser(c)
		if !ok {
			fail(c, http.StatusUnauthorized, "Не авторизован")
			return
		}
		fn(c, u)
	}
}

// page — стандартный ответ-список.
type page[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
	Page  int   `json:"page"`
	Limit int   `json:"limit"`
}

type items[T any] struct {
	Items []T `json:"items"`
}

func fail(c *gin.Context, code int, detail string) {
	c.AbortWithStatusJSON(code, gin.H{"detail": detail})
}

func (h *Handler) internalError(c *gin.Context, err error) {
	h.log.Error("внутренняя ошибка", "method", c.Request.Method, "route", c.FullPath(), "error", err)
	fail(c, http.StatusInternalServerError, "Внутренняя ошибка сервера")
}

func isNotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }

// pathID разбирает UUID из пути; некорректный UUID — 404 (такого объекта не может быть).
func pathID(c *gin.Context, name, notFound string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		fail(c, http.StatusNotFound, notFound)
		return uuid.Nil, false
	}
	return id, true
}

// pagination разбирает page/limit из query.
func pagination(c *gin.Context) (int, int, bool) {
	p, l, err := params.Pagination(c.Query("page"), c.Query("limit"))
	if err != nil {
		fail(c, http.StatusUnprocessableEntity, err.Error())
		return 0, 0, false
	}
	return p, l, true
}

// bindJSON декодирует тело-объект JSON. allowEmpty — пустое тело допустимо.
func bindJSON(c *gin.Context, dst any, allowEmpty bool) bool {
	if c.Request.Body == nil {
		if allowEmpty {
			return true
		}
		fail(c, http.StatusUnprocessableEntity, "Ожидается JSON-объект в теле запроса")
		return false
	}
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(c, http.StatusRequestEntityTooLarge, "Слишком большое тело запроса")
			return false
		}
		fail(c, http.StatusBadRequest, "Не удалось прочитать тело запроса")
		return false
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		if allowEmpty {
			return true
		}
		fail(c, http.StatusUnprocessableEntity, "Ожидается JSON-объект в теле запроса")
		return false
	}
	if raw[0] != '{' {
		fail(c, http.StatusUnprocessableEntity, "Ожидается JSON-объект в теле запроса")
		return false
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) && typeErr.Field != "" {
			fail(c, http.StatusUnprocessableEntity, fmt.Sprintf("Некорректный тип поля %s", typeErr.Field))
			return false
		}
		fail(c, http.StatusUnprocessableEntity, "Некорректный JSON в теле запроса")
		return false
	}
	return true
}

// Opt — поле JSON с признаком присутствия: отличает «не передано» от null.
type Opt[T any] struct {
	Set  bool
	Null bool
	V    T
}

// UnmarshalJSON вызывается encoding/json и для литерала null.
func (o *Opt[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(bytes.TrimSpace(b)) == "null" {
		o.Null = true
		var zero T
		o.V = zero
		return nil
	}
	o.Null = false
	return json.Unmarshal(b, &o.V)
}

// Value — значение, если поле передано и не null.
func (o Opt[T]) Value() (T, bool) { return o.V, o.Set && !o.Null }

func invalid(c *gin.Context, field, msg string) {
	fail(c, http.StatusUnprocessableEntity, fmt.Sprintf("Поле %s: %s", field, msg))
}

// optUUID разбирает необязательный UUID (null или "" → nil).
func optUUID(o Opt[string]) (*uuid.UUID, error) {
	s, ok := o.Value()
	if !ok || strings.TrimSpace(s) == "" {
		return nil, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(s))
	if err != nil {
		return nil, errors.New("ожидается UUID")
	}
	return &id, nil
}

// optDateTime разбирает необязательную дату-время (null или "" → nil).
func optDateTime(o Opt[string]) (*time.Time, error) {
	s, ok := o.Value()
	if !ok || strings.TrimSpace(s) == "" {
		return nil, nil
	}
	t, err := params.ParseDateTime(s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// queryRange разбирает границы периода из query как полуинтервал [from, to).
func queryRange(c *gin.Context, fromKey, toKey string) (from, to *time.Time, ok bool) {
	if s := c.Query(fromKey); s != "" {
		t, err := params.RangeBound(s, false)
		if err != nil {
			invalid(c, fromKey, err.Error())
			return nil, nil, false
		}
		from = &t
	}
	if s := c.Query(toKey); s != "" {
		t, err := params.RangeBound(s, true)
		if err != nil {
			invalid(c, toKey, err.Error())
			return nil, nil, false
		}
		to = &t
	}
	if from != nil && to != nil && !to.After(*from) {
		fail(c, http.StatusUnprocessableEntity, fmt.Sprintf("Параметр %s должен быть позже %s", toKey, fromKey))
		return nil, nil, false
	}
	return from, to, true
}

func ts(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }

func tsp(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := ts(*t)
	return &v
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
