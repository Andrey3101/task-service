// Package params содержит чистые функции разбора и валидации входных данных
// (без зависимостей от HTTP-фреймворка и БД).
package params

import (
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	DefaultLimit = 20
	MaxLimit     = 100
	DateLayout   = "2006-01-02"
)

// Допустимые значения перечислений из docs/API.md.
var (
	TaskStatuses   = []string{"todo", "in_progress", "done", "cancelled"}
	TaskPriorities = []string{"low", "medium", "high", "urgent"}
	DateTypes      = []string{"deadline", "birthday", "holiday", "payment", "other"}
	Recurrences    = []string{"daily", "weekly", "monthly"}
)

// OneOf проверяет принадлежность значения перечислению.
func OneOf(v string, allowed []string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// Pagination разбирает page/limit: page ≥ 1, 1 ≤ limit ≤ 100, по умолчанию 1 и 20.
func Pagination(pageRaw, limitRaw string) (page, limit int, err error) {
	page, limit = 1, DefaultLimit
	if s := strings.TrimSpace(pageRaw); s != "" {
		page, err = strconv.Atoi(s)
		if err != nil || page < 1 {
			return 0, 0, errors.New("Параметр page должен быть целым числом ≥ 1")
		}
	}
	if s := strings.TrimSpace(limitRaw); s != "" {
		limit, err = strconv.Atoi(s)
		if err != nil || limit < 1 || limit > MaxLimit {
			return 0, 0, errors.New("Параметр limit должен быть целым числом от 1 до 100")
		}
	}
	return page, limit, nil
}

// Offset — смещение для страницы.
func Offset(page, limit int) int { return (page - 1) * limit }

// ParseDate разбирает дату "YYYY-MM-DD" (полночь UTC).
func ParseDate(s string) (time.Time, error) {
	t, err := time.ParseInLocation(DateLayout, strings.TrimSpace(s), time.UTC)
	if err != nil {
		return time.Time{}, errors.New("ожидается дата в формате YYYY-MM-DD")
	}
	return t, nil
}

// ParseDateTime принимает ISO 8601 (RFC 3339, с дробными секундами или без)
// или дату "YYYY-MM-DD". Дата без времени трактуется как 12:00 UTC этого дня —
// так календарная дата одинакова во всех часовых поясах от UTC-11 до UTC+11.
func ParseDateTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.ParseInLocation("2006-01-02T15:04:05", s, time.UTC); err == nil {
		return t, nil
	}
	if d, err := ParseDate(s); err == nil {
		return DateAtNoon(d), nil
	}
	return time.Time{}, errors.New("ожидается дата-время ISO 8601 (например 2026-10-02T12:00:00Z) или дата YYYY-MM-DD")
}

// DateAtNoon возвращает 12:00 UTC календарного дня d.
func DateAtNoon(d time.Time) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, time.UTC)
}

// RangeBound разбирает границу периода фильтра. Для даты без времени нижняя граница —
// начало дня, верхняя (end=true) — начало следующего дня (интервал [from, to) включает весь день to).
func RangeBound(s string, end bool) (time.Time, error) {
	s = strings.TrimSpace(s)
	if d, err := ParseDate(s); err == nil {
		if end {
			return d.AddDate(0, 0, 1), nil
		}
		return d, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, errors.New("ожидается дата YYYY-MM-DD или дата-время ISO 8601")
	}
	return t.UTC(), nil
}

// CleanText обрезает пробелы и проверяет длину в символах (0 = без ограничения сверху).
func CleanText(s string, minLen, maxLen int) (string, bool) {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	if n < minLen || (maxLen > 0 && n > maxLen) {
		return s, false
	}
	return s, true
}

// CleanList нормализует список строк: обрезает пробелы, убирает пустые и дубликаты,
// проверяет количество и длину элементов. Всегда возвращает не-nil срез.
func CleanList(in []string, maxItems, maxLen int) ([]string, bool) {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if utf8.RuneCountInString(v) > maxLen {
			return nil, false
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	if len(out) > maxItems {
		return nil, false
	}
	return out, true
}

// LikePattern экранирует спецсимволы LIKE (\ % _) и оборачивает строку в %...%.
func LikePattern(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}

// AddMonthsClamped прибавляет n месяцев, не перескакивая через конец месяца
// (31 января + 1 месяц = 28/29 февраля, а не 2/3 марта).
func AddMonthsClamped(t time.Time, n int) time.Time {
	y, m, d := t.Date()
	first := time.Date(y, m+time.Month(n), 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
	last := first.AddDate(0, 1, -1).Day()
	if d > last {
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}

// RecurrenceDates возвращает count дат, начиная со start, с шагом recurrence.
func RecurrenceDates(start time.Time, recurrence string, count int) ([]time.Time, error) {
	if count < 1 || count > 52 {
		return nil, errors.New("count должен быть от 1 до 52")
	}
	out := make([]time.Time, 0, count)
	for i := 0; i < count; i++ {
		switch recurrence {
		case "daily":
			out = append(out, start.AddDate(0, 0, i))
		case "weekly":
			out = append(out, start.AddDate(0, 0, 7*i))
		case "monthly":
			out = append(out, AddMonthsClamped(start, i))
		default:
			return nil, errors.New("recurrence должен быть daily, weekly или monthly")
		}
	}
	return out, nil
}

// TaskSortOrder отображает параметр sort в ORDER BY (белый список, без пользовательского SQL).
func TaskSortOrder(sort string) (string, bool) {
	switch strings.TrimSpace(sort) {
	case "", "-created_at":
		return "created_at DESC, id DESC", true
	case "created_at":
		return "created_at ASC, id ASC", true
	case "due_date":
		return "due_date ASC NULLS LAST, created_at DESC, id DESC", true
	case "-due_date":
		return "due_date DESC NULLS LAST, created_at DESC, id DESC", true
	case "priority":
		return "CASE priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END ASC, created_at DESC, id DESC", true
	case "-updated_at":
		return "updated_at DESC, id DESC", true
	}
	return "", false
}

// DurationMinutes — целые минуты между start и end (не меньше 0).
func DurationMinutes(start, end time.Time) int {
	d := end.Sub(start)
	if d < 0 {
		return 0
	}
	return int(d / time.Minute)
}
