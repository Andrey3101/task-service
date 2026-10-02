package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"clerkai/task-service/internal/auth"
	"clerkai/task-service/internal/config"
	"clerkai/task-service/internal/models"
	"clerkai/task-service/internal/notify"
)

var testSecret = []byte("api-test-secret-api-test-secret-123")

type nopSender struct{ sent []notify.Notification }

func (s *nopSender) Send(context.Context, notify.Notification) error { return nil }
func (s *nopSender) SendAsync(n notify.Notification)                 { s.sent = append(s.sent, n) }

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// newTestRouter — роутер без БД: проверяются маршрутизация, auth и обработчики до обращения к БД.
func newTestRouter(t *testing.T, health func(context.Context) error) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r, err := NewRouter(RouterConfig{
		JWTSecret: testSecret, TrustedProxies: config.DefaultTrustedProxies, Log: discardLog(), HealthCheck: health,
	}, NewHandler(nil, &nopSender{}, discardLog()))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func token(t *testing.T) string {
	t.Helper()
	now := time.Now()
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": uuid.NewString(), "company_id": uuid.NewString(), "company_type": "ip", "role": "user",
		"email": "a@b.c", "name": "Тест", "typ": "access", "iss": auth.Issuer,
		"iat": now.Unix(), "exp": now.Add(time.Minute).Unix(),
	}).SignedString(testSecret)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func call(r http.Handler, method, path, bearer, body string) (int, map[string]any) {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

// Все маршруты контракта зарегистрированы (иначе был бы 404 без auth) и закрыты авторизацией.
func TestRoutesRequireAuth(t *testing.T) {
	r := newTestRouter(t, nil)
	id, cid := uuid.NewString(), uuid.NewString()
	routes := [][2]string{
		{"GET", "/api/tasks"}, {"POST", "/api/tasks"},
		{"GET", "/api/tasks/" + id}, {"PUT", "/api/tasks/" + id}, {"DELETE", "/api/tasks/" + id},
		{"GET", "/api/tasks/stats"}, {"GET", "/api/tasks/activity"}, {"GET", "/api/tasks/suggestions"},
		{"POST", "/api/tasks/bulk-assign"}, {"POST", "/api/tasks/recurring"}, {"POST", "/api/tasks/tax-reminders"},
		{"GET", "/api/tasks/" + id + "/comments"}, {"POST", "/api/tasks/" + id + "/comments"},
		{"PUT", "/api/tasks/" + id + "/comments/" + cid}, {"DELETE", "/api/tasks/" + id + "/comments/" + cid},
		{"POST", "/api/tasks/" + id + "/time/start"},
		{"POST", "/api/time/stop"}, {"GET", "/api/time/active"}, {"GET", "/api/time/report"},
		{"GET", "/api/meetings"}, {"POST", "/api/meetings"},
		{"GET", "/api/meetings/" + id}, {"PUT", "/api/meetings/" + id}, {"DELETE", "/api/meetings/" + id},
		{"GET", "/api/important-dates"}, {"POST", "/api/important-dates"},
		{"GET", "/api/important-dates/" + id}, {"PUT", "/api/important-dates/" + id}, {"DELETE", "/api/important-dates/" + id},
	}
	for _, rt := range routes {
		code, body := call(r, rt[0], rt[1], "", "")
		if code != http.StatusUnauthorized || body["detail"] != "Не авторизован" {
			t.Errorf("%s %s: %d %v", rt[0], rt[1], code, body)
		}
	}
}

func TestNotFoundAndHealth(t *testing.T) {
	r := newTestRouter(t, func(context.Context) error { return nil })
	if code, body := call(r, "GET", "/api/unknown", "", ""); code != http.StatusNotFound || body["detail"] == nil {
		t.Errorf("404: %d %v", code, body)
	}
	if code, body := call(r, "GET", "/health", "", ""); code != http.StatusOK || body["status"] != "ok" {
		t.Errorf("health: %d %v", code, body)
	}
	down := newTestRouter(t, func(context.Context) error { return errors.New("down") })
	if code, _ := call(down, "GET", "/health", "", ""); code != http.StatusServiceUnavailable {
		t.Errorf("health down: %d", code)
	}
}

// Статические сегменты и :id не конфликтуют и разрешаются в нужные обработчики.
func TestStaticVsParamDispatch(t *testing.T) {
	r := newTestRouter(t, nil)
	tok := token(t)
	// :id с не-UUID → обработчик задачи отвечает 404 «Задача не найдена» без обращения к БД.
	for _, p := range []string{"/api/tasks/not-a-uuid", "/api/tasks/stats/comments", "/api/tasks/statsx"} {
		if code, body := call(r, "GET", p, tok, ""); code != http.StatusNotFound || body["detail"] != msgTaskNotFound {
			t.Errorf("GET %s: %d %v", p, code, body)
		}
	}
	if code, body := call(r, "POST", "/api/tasks/recurring/time/start", tok, ""); code != http.StatusNotFound || body["detail"] != msgTaskNotFound {
		t.Errorf("POST time/start: %d %v", code, body)
	}
	// /stats попадает в свой обработчик (а не в getTask): БД нет → паника → recovery → 500.
	if code, body := call(r, "GET", "/api/tasks/stats", tok, ""); code != http.StatusInternalServerError || body["detail"] != "Внутренняя ошибка сервера" {
		t.Errorf("GET /api/tasks/stats: %d %v", code, body)
	}
	// Валидация до обращения к БД.
	if code, _ := call(r, "GET", "/api/tasks?limit=500", tok, ""); code != http.StatusUnprocessableEntity {
		t.Errorf("limit=500: %d", code)
	}
	if code, _ := call(r, "GET", "/api/tasks?sort=evil", tok, ""); code != http.StatusUnprocessableEntity {
		t.Errorf("sort=evil: %d", code)
	}
	if code, _ := call(r, "POST", "/api/tasks", tok, `{"description":"x"}`); code != http.StatusUnprocessableEntity {
		t.Errorf("POST без title: %d", code)
	}
	if code, _ := call(r, "POST", "/api/tasks", tok, `[1]`); code != http.StatusUnprocessableEntity {
		t.Errorf("POST массив: %d", code)
	}
	if code, body := call(r, "POST", "/api/tasks/tax-reminders", tok, `{"year":1990}`); code != http.StatusUnprocessableEntity {
		t.Errorf("tax year: %d %v", code, body)
	}
	if code, _ := call(r, "POST", "/api/tasks/bulk-assign", tok, `{"task_ids":[],"assignee_id":"x"}`); code != http.StatusUnprocessableEntity {
		t.Errorf("bulk empty: %d", code)
	}
	if code, _ := call(r, "POST", "/api/tasks/recurring", tok, `{"title":"x","recurrence":"daily","count":53,"start_date":"2026-01-01"}`); code != http.StatusUnprocessableEntity {
		t.Errorf("recurring count: %d", code)
	}
	if code, _ := call(r, "POST", "/api/time/stop", tok, `{"task_id":"nope"}`); code != http.StatusUnprocessableEntity {
		t.Errorf("stop bad task_id: %d", code)
	}
	if code, _ := call(r, "GET", "/api/meetings?from=bad", tok, ""); code != http.StatusUnprocessableEntity {
		t.Errorf("meetings from: %d", code)
	}
	if code, _ := call(r, "GET", "/api/important-dates?to=2026-13-01", tok, ""); code != http.StatusUnprocessableEntity {
		t.Errorf("dates to: %d", code)
	}
	if code, _ := call(r, "GET", "/api/time/report?user_id="+uuid.NewString(), tok, ""); code != http.StatusForbidden {
		t.Errorf("report чужой user_id для role=user: %d", code)
	}
}

func TestOptUnmarshal(t *testing.T) {
	var in taskInput
	if err := json.Unmarshal([]byte(`{"title":"A","due_date":null}`), &in); err != nil {
		t.Fatal(err)
	}
	if !in.Title.Set || in.Title.Null || in.Title.V != "A" {
		t.Errorf("title: %+v", in.Title)
	}
	if !in.DueDate.Set || !in.DueDate.Null {
		t.Errorf("due_date: %+v", in.DueDate)
	}
	if in.Status.Set {
		t.Errorf("status не передан: %+v", in.Status)
	}
	if err := json.Unmarshal([]byte(`{"title":5}`), &in); err == nil {
		t.Error("ожидалась ошибка типа")
	}
}

func TestApplyTaskInput(t *testing.T) {
	u := auth.User{ID: uuid.New(), CompanyID: uuid.New(), Role: "user", Name: "Автор"}
	other := uuid.New()

	task := newTask(u)
	var in taskInput
	_ = json.Unmarshal([]byte(`{"title":"  Сдать отчёт ","due_date":"2026-10-05","assignee_id":"`+other.String()+`","assignee_name":"Пётр","tags":["a","a"," b "]}`), &in)
	changed, field, msg := applyTaskInput(&task, in, u, true)
	if field != "" || !changed {
		t.Fatalf("create: %s %s changed=%v", field, msg, changed)
	}
	if task.Title != "Сдать отчёт" || task.Status != "todo" || task.Priority != "medium" ||
		task.DueDate == nil || task.DueDate.Format(time.RFC3339) != "2026-10-05T12:00:00Z" ||
		task.AssigneeName == nil || *task.AssigneeName != "Пётр" || len(task.Tags) != 2 {
		t.Errorf("create result: %+v", task)
	}

	// Частичное обновление: смена исполнителя без имени сбрасывает имя; null снимает срок.
	in = taskInput{}
	_ = json.Unmarshal([]byte(`{"assignee_id":"`+u.ID.String()+`","due_date":null,"status":"done"}`), &in)
	changed, field, _ = applyTaskInput(&task, in, u, false)
	if field != "" || !changed || task.DueDate != nil || task.AssigneeName == nil || *task.AssigneeName != "Автор" {
		t.Errorf("update: field=%s changed=%v task=%+v", field, changed, task)
	}

	for _, body := range []string{`{"status":"unknown"}`, `{"priority":"low1"}`, `{"title":""}`, `{"title":null}`,
		`{"due_date":"завтра"}`, `{"assignee_id":"x"}`, `{"tags":["` + strings.Repeat("x", 51) + `"]}`} {
		in = taskInput{}
		_ = json.Unmarshal([]byte(body), &in)
		if _, field, _ := applyTaskInput(&task, in, u, false); field == "" {
			t.Errorf("%s: ожидалась ошибка валидации", body)
		}
	}
}

func TestSyncCompletedAtAndPermissions(t *testing.T) {
	h := NewHandler(nil, &nopSender{}, discardLog())
	fixed := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	h.now = func() time.Time { return fixed }
	task := models.Task{Status: "done"}
	h.syncCompletedAt(&task)
	if task.CompletedAt == nil || !task.CompletedAt.Equal(fixed) {
		t.Errorf("completed_at: %v", task.CompletedAt)
	}
	task.Status = "in_progress"
	h.syncCompletedAt(&task)
	if task.CompletedAt != nil {
		t.Error("completed_at должен сбрасываться")
	}

	author, assignee, stranger := uuid.New(), uuid.New(), uuid.New()
	task = models.Task{CreatedBy: author, AssigneeID: &assignee}
	if !canModifyTask(auth.User{ID: author, Role: "user"}, &task) ||
		!canModifyTask(auth.User{ID: assignee, Role: "user"}, &task) ||
		canModifyTask(auth.User{ID: stranger, Role: "user"}, &task) ||
		!canModifyTask(auth.User{ID: stranger, Role: "admin"}, &task) ||
		!canModifyTask(auth.User{ID: stranger, Role: "owner"}, &task) {
		t.Error("canModifyTask")
	}
}

func TestNotifyAssigned(t *testing.T) {
	s := &nopSender{}
	h := NewHandler(nil, s, discardLog())
	u := auth.User{ID: uuid.New(), CompanyID: uuid.New(), Name: "Анна"}
	other := uuid.New()
	task := models.Task{ID: uuid.New(), CompanyID: u.CompanyID, Title: "T", AssigneeID: &other}
	h.notifyAssigned(u, &task)
	task.AssigneeID = &u.ID
	h.notifyAssigned(u, &task) // себе — не уведомляем
	if len(s.sent) != 1 || s.sent[0].UserID != other || s.sent[0].Link != "/tasks/"+task.ID.String() || s.sent[0].Type != "task" {
		t.Errorf("sent: %+v", s.sent)
	}
}

func TestSuggestionReason(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	past, soon, later := now.Add(-time.Hour), now.Add(24*time.Hour), now.Add(72*time.Hour)
	cases := []struct {
		task models.Task
		want string
	}{
		{models.Task{Status: "todo", DueDate: &past}, "Просрочена"},
		{models.Task{Status: "in_progress", DueDate: &soon}, "Срок истекает в ближайшие 2 дня"},
		{models.Task{Status: "todo", Priority: "urgent", DueDate: &later}, "Срочная задача ещё не начата"},
		{models.Task{Status: "todo", Priority: "high"}, "Задача с высоким приоритетом ещё не начата"},
		{models.Task{Status: "in_progress", Priority: "high"}, ""},
		{models.Task{Status: "done", DueDate: &past}, ""},
	}
	for i, c := range cases {
		if got := suggestionReason(&c.task, now); got != c.want {
			t.Errorf("case %d: %q, want %q", i, got, c.want)
		}
	}
}

func TestBuildActivityAndWeek(t *testing.T) {
	since := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	items := buildActivity(since, 3, []dayCount{{"2026-10-01", 2}}, []dayCount{{"2026-10-02", 1}})
	if len(items) != 3 || items[0].Date != "2026-09-30" || items[1].Created != 2 || items[2].Completed != 1 || items[0].Created != 0 {
		t.Errorf("activity: %+v", items)
	}
	// 2026-10-04 — воскресенье, неделя начинается в понедельник 2026-09-28.
	if got := startOfWeekUTC(time.Date(2026, 10, 4, 23, 0, 0, 0, time.UTC)); got.Format("2006-01-02") != "2026-09-28" {
		t.Errorf("startOfWeek: %s", got)
	}
	if got := startOfWeekUTC(time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC)); got.Format("2006-01-02") != "2026-09-28" {
		t.Errorf("startOfWeek monday: %s", got)
	}
}

func TestSummarizeAndDTO(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	rep := summarize([]TimeEntryDTO{
		{TaskID: a, TaskTitle: "A", DurationMinutes: 10},
		{TaskID: b, TaskTitle: "B", DurationMinutes: 30},
		{TaskID: a, TaskTitle: "A", DurationMinutes: 5},
	})
	if rep.TotalMinutes != 45 || len(rep.ByTask) != 2 || rep.ByTask[0].TaskID != b || rep.ByTask[1].Minutes != 15 {
		t.Errorf("summarize: %+v", rep)
	}
	empty := summarize([]TimeEntryDTO{})
	raw, _ := json.Marshal(empty)
	if string(raw) != `{"total_minutes":0,"by_task":[],"entries":[]}` {
		t.Errorf("empty report json: %s", raw)
	}

	started := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	e := models.TimeEntry{StartedAt: started}
	if d := timeEntryDTO(&e, "T", started.Add(42*time.Minute)); d.DurationMinutes != 42 || d.EndedAt != nil {
		t.Errorf("active entry dto: %+v", d)
	}

	date := models.ImportantDate{Date: time.Date(2026, 12, 31, 0, 0, 0, 0, time.FixedZone("MSK", 3*3600))}
	raw, _ = json.Marshal(importantDateDTO(&date))
	if !strings.Contains(string(raw), `"date":"2026-12-31"`) {
		t.Errorf("important date json: %s", raw)
	}

	task := models.Task{CreatedAt: time.Date(2026, 10, 2, 15, 0, 0, 999, time.FixedZone("MSK", 3*3600))}
	raw, _ = json.Marshal(taskDTO(&task, 3, 7))
	s := string(raw)
	for _, want := range []string{`"created_at":"2026-10-02T12:00:00Z"`, `"tags":[]`, `"due_date":null`, `"assignee_id":null`,
		`"comments_count":3`, `"total_minutes":7`, `"completed_at":null`} {
		if !strings.Contains(s, want) {
			t.Errorf("task json missing %s: %s", want, s)
		}
	}
}
