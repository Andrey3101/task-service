// Package notify отправляет уведомления в notification-service
// (POST /api/internal/notifications с X-Service-Token) и запускает фоновые напоминания.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Notification — тело запроса к notification-service.
type Notification struct {
	UserID    uuid.UUID `json:"user_id"`
	CompanyID uuid.UUID `json:"company_id"`
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	Type      string    `json:"type"`
	Link      string    `json:"link"`
}

// Sender — то, что нужно хендлерам и фоновой задаче.
type Sender interface {
	Send(ctx context.Context, n Notification) error
	SendAsync(n Notification)
}

// Client — HTTP-клиент notification-service. Ошибки только логируются вызывающим кодом.
type Client struct {
	endpoint string
	token    string
	http     *http.Client
	log      *slog.Logger
	wg       sync.WaitGroup
}

// NewClient создаёт клиента; без serviceToken отправка отключена (Send возвращает nil).
func NewClient(baseURL, serviceToken string, log *slog.Logger) *Client {
	return &Client{
		endpoint: baseURL + "/api/internal/notifications",
		token:    serviceToken,
		http:     &http.Client{Timeout: 5 * time.Second},
		log:      log,
	}
}

// Enabled — задан ли SERVICE_TOKEN.
func (c *Client) Enabled() bool { return c.token != "" }

// Send синхронно отправляет уведомление.
func (c *Client) Send(ctx context.Context, n Notification) error {
	if !c.Enabled() {
		return nil
	}
	if n.Type == "" {
		n.Type = "task"
	}
	body, err := json.Marshal(n)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Service-Token", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("notification-service ответил %d", resp.StatusCode)
	}
	return nil
}

// SendAsync отправляет уведомление в фоне, не задерживая HTTP-ответ.
func (c *Client) SendAsync(n Notification) {
	if !c.Enabled() {
		return
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		if err := c.Send(ctx, n); err != nil {
			c.log.Warn("не удалось отправить уведомление", "user_id", n.UserID, "link", n.Link, "error", err)
		}
	}()
}

// Wait дожидается фоновых отправок (при остановке сервиса) или отмены ctx.
func (c *Client) Wait(ctx context.Context) {
	done := make(chan struct{})
	go func() { c.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}
