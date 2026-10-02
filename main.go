// task-service ClerkAI: задачи, учёт времени, встречи, важные даты (docs/API.md §5).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"clerkai/task-service/internal/api"
	"clerkai/task-service/internal/config"
	"clerkai/task-service/internal/models"
	"clerkai/task-service/internal/notify"
)

func main() {
	if err := run(); err != nil {
		slog.Error("task-service остановлен с ошибкой", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	db, err := openDB(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	if err := db.WithContext(ctx).AutoMigrate(models.All()...); err != nil {
		return err
	}

	notifier := notify.NewClient(cfg.NotificationServiceURL, cfg.ServiceToken, log)
	if !notifier.Enabled() {
		log.Warn("SERVICE_TOKEN не задан — уведомления и фоновые напоминания отключены")
	}

	router, err := api.NewRouter(api.RouterConfig{
		JWTSecret:      []byte(cfg.JWTSecret),
		TrustedProxies: cfg.TrustedProxies,
		Log:            log,
		HealthCheck:    sqlDB.PingContext,
		ServiceToken:   cfg.ServiceToken,
	}, api.NewHandler(db, notifier, log))
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              net.JoinHostPort("", cfg.Port),
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()
	var workers sync.WaitGroup
	if notifier.Enabled() {
		log.Info("фоновые напоминания включены", "interval_sec", int(cfg.RemindersInterval/time.Second))
		workers.Add(1)
		go func() {
			defer workers.Done()
			notify.RunReminders(workerCtx, db, notifier, cfg.RemindersInterval, log)
		}()
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Info("task-service запущен", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case <-ctx.Done():
		stop() // повторный сигнал завершит процесс сразу
		log.Info("получен сигнал остановки")
	case err := <-serveErr:
		cancelWorkers()
		workers.Wait()
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	cancelWorkers()
	workers.Wait()
	notifier.Wait(shutdownCtx)
	log.Info("task-service остановлен")
	return err
}

// openDB подключается к PostgreSQL, повторяя попытки, пока БД поднимается (до ~60 с).
func openDB(ctx context.Context, dsn string, log *slog.Logger) (*gorm.DB, error) {
	gormLog := logger.New(slog.NewLogLogger(log.Handler(), slog.LevelWarn), logger.Config{
		SlowThreshold:             500 * time.Millisecond,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: true,
		Colorful:                  false,
	})
	var lastErr error
	for attempt := 1; attempt <= 30; attempt++ {
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormLog})
		if err == nil {
			return db, nil
		}
		if db != nil {
			if sqlDB, e := db.DB(); e == nil {
				_ = sqlDB.Close()
			}
		}
		lastErr = err
		log.Warn("база данных недоступна, повтор через 2 с", "attempt", attempt)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil, lastErr
}
