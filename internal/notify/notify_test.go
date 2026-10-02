package notify

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestClientSend(t *testing.T) {
	var got map[string]any
	var token, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, token = r.URL.Path, r.Header.Get("X-Service-Token")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := NewClient(srv.URL, "svc-token", log)
	user, company := uuid.New(), uuid.New()
	err := c.Send(context.Background(), Notification{UserID: user, CompanyID: company, Title: "T", Message: "M", Link: "/tasks/1"})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/internal/notifications" || token != "svc-token" {
		t.Errorf("path=%s token=%s", path, token)
	}
	if got["user_id"] != user.String() || got["company_id"] != company.String() || got["type"] != "task" ||
		got["title"] != "T" || got["message"] != "M" || got["link"] != "/tasks/1" {
		t.Errorf("body: %v", got)
	}

	c.SendAsync(Notification{UserID: user, CompanyID: company, Title: "A"})
	c.Wait(context.Background())
	if got["title"] != "A" {
		t.Errorf("async body: %v", got)
	}
}

func TestClientErrorsAndDisabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := NewClient(srv.URL, "x", log).Send(context.Background(), Notification{}); err == nil {
		t.Error("ожидалась ошибка на 500")
	}
	disabled := NewClient("http://127.0.0.1:1", "", log)
	if disabled.Enabled() {
		t.Error("без токена клиент должен быть выключен")
	}
	if err := disabled.Send(context.Background(), Notification{}); err != nil {
		t.Errorf("выключенный клиент: %v", err)
	}
}
