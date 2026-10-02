package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"

	"clerkai/task-service/internal/auth"
)

// maxBodyBytes — лимит тела запроса.
const maxBodyBytes = 1 << 20

// RouterConfig — параметры сборки роутера.
type RouterConfig struct {
	JWTSecret      []byte
	TrustedProxies []string
	Log            *slog.Logger
	// HealthCheck проверяет зависимости (пинг БД) для GET /health.
	HealthCheck func(ctx context.Context) error
}

// NewRouter собирает gin.Engine: глобальные мидлвари → /health → группа /api с auth → маршруты.
func NewRouter(cfg RouterConfig, h *Handler) (*gin.Engine, error) {
	r := gin.New()
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, err
	}
	r.Use(recovery(cfg.Log), requestLogger(cfg.Log), limitBody(maxBodyBytes))
	r.NoRoute(func(c *gin.Context) { fail(c, http.StatusNotFound, "Не найдено") })

	r.GET("/health", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if cfg.HealthCheck != nil {
			if err := cfg.HealthCheck(ctx); err != nil {
				cfg.Log.Warn("health: база данных недоступна", "error", err)
				fail(c, http.StatusServiceUnavailable, "База данных недоступна")
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	apiGroup := r.Group("/api")
	apiGroup.Use(auth.Middleware(cfg.JWTSecret)) // до регистрации маршрутов — иначе не применится
	Register(apiGroup, h)
	return r, nil
}

// Register регистрирует маршруты docs/API.md §5. Параметры: :id — ресурс, :comment_id — комментарий.
// Статические сегменты (/stats, /activity …) соседствуют с /:id — в gin ≥ 1.8 это допустимо.
func Register(g *gin.RouterGroup, h *Handler) {
	tasks := g.Group("/tasks")
	tasks.GET("", h.wrap(h.listTasks))
	tasks.POST("", h.wrap(h.createTask))
	tasks.GET("/stats", h.wrap(h.taskStats))
	tasks.GET("/activity", h.wrap(h.taskActivity))
	tasks.GET("/suggestions", h.wrap(h.taskSuggestions))
	tasks.POST("/bulk-assign", h.wrap(h.bulkAssign))
	tasks.POST("/recurring", h.wrap(h.createRecurring))
	tasks.POST("/tax-reminders", h.wrap(h.createTaxReminders))
	tasks.GET("/:id", h.wrap(h.getTask))
	tasks.PUT("/:id", h.wrap(h.updateTask))
	tasks.DELETE("/:id", h.wrap(h.deleteTask))
	tasks.GET("/:id/comments", h.wrap(h.listComments))
	tasks.POST("/:id/comments", h.wrap(h.createComment))
	tasks.PUT("/:id/comments/:comment_id", h.wrap(h.updateComment))
	tasks.DELETE("/:id/comments/:comment_id", h.wrap(h.deleteComment))
	tasks.POST("/:id/time/start", h.wrap(h.startTimer))

	timeGroup := g.Group("/time")
	timeGroup.POST("/stop", h.wrap(h.stopTimer))
	timeGroup.GET("/active", h.wrap(h.activeTimers))
	timeGroup.GET("/report", h.wrap(h.timeReport))

	meetings := g.Group("/meetings")
	meetings.GET("", h.wrap(h.listMeetings))
	meetings.POST("", h.wrap(h.createMeeting))
	meetings.GET("/:id", h.wrap(h.getMeeting))
	meetings.PUT("/:id", h.wrap(h.updateMeeting))
	meetings.DELETE("/:id", h.wrap(h.deleteMeeting))

	dates := g.Group("/important-dates")
	dates.GET("", h.wrap(h.listDates))
	dates.POST("", h.wrap(h.createDate))
	dates.GET("/:id", h.wrap(h.getDate))
	dates.PUT("/:id", h.wrap(h.updateDate))
	dates.DELETE("/:id", h.wrap(h.deleteDate))
}

// recovery превращает panic в 500 {"detail"} и пишет стек в slog (вывод gin в stderr отключён).
func recovery(log *slog.Logger) gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, rec any) {
		log.Error("panic в обработчике", "method", c.Request.Method, "route", c.FullPath(),
			"panic", fmt.Sprint(rec), "stack", string(debug.Stack()))
		fail(c, http.StatusInternalServerError, "Внутренняя ошибка сервера")
	})
}

func requestLogger(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		status := c.Writer.Status()
		level := slog.LevelInfo
		switch {
		case status >= 500:
			level = slog.LevelError
		case c.Request.URL.Path == "/health":
			level = slog.LevelDebug
		}
		log.LogAttrs(c.Request.Context(), level, "http",
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", status),
			slog.Duration("latency", time.Since(start)),
			slog.String("ip", c.ClientIP()),
		)
	}
}

func limitBody(n int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, n)
		}
		c.Next()
	}
}
