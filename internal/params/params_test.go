package params

import (
	"testing"
	"time"
)

func TestPagination(t *testing.T) {
	cases := []struct {
		page, limit         string
		wantPage, wantLimit int
		wantErr             bool
	}{
		{"", "", 1, 20, false},
		{"3", "100", 3, 100, false},
		{"0", "", 0, 0, true},
		{"1", "101", 0, 0, true},
		{"x", "", 0, 0, true},
		{"", "0", 0, 0, true},
	}
	for _, c := range cases {
		p, l, err := Pagination(c.page, c.limit)
		if (err != nil) != c.wantErr || p != c.wantPage || l != c.wantLimit {
			t.Errorf("Pagination(%q,%q) = %d,%d,%v", c.page, c.limit, p, l, err)
		}
	}
	if Offset(3, 20) != 40 {
		t.Error("Offset(3,20) != 40")
	}
}

func TestParseDateTime(t *testing.T) {
	cases := map[string]string{
		"2026-10-02T12:00:00Z":      "2026-10-02T12:00:00Z",
		"2026-10-02T15:00:00+03:00": "2026-10-02T12:00:00Z",
		"2026-10-02T12:00:00.123Z":  "2026-10-02T12:00:00.123Z",
		"2026-10-02T12:00:00":       "2026-10-02T12:00:00Z",
		"2026-10-02":                "2026-10-02T12:00:00Z",
	}
	for in, want := range cases {
		got, err := ParseDateTime(in)
		if err != nil || got.Format(time.RFC3339Nano) != want {
			t.Errorf("ParseDateTime(%q) = %v, %v; want %s", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "02.10.2026", "2026-13-01", "tomorrow"} {
		if _, err := ParseDateTime(bad); err == nil {
			t.Errorf("ParseDateTime(%q) must fail", bad)
		}
	}
}

func TestRangeBound(t *testing.T) {
	from, _ := RangeBound("2026-10-02", false)
	to, _ := RangeBound("2026-10-02", true)
	if from.Format(time.RFC3339) != "2026-10-02T00:00:00Z" || to.Format(time.RFC3339) != "2026-10-03T00:00:00Z" {
		t.Errorf("RangeBound date: %v %v", from, to)
	}
	dt, err := RangeBound("2026-10-02T10:30:00+03:00", true)
	if err != nil || dt.Format(time.RFC3339) != "2026-10-02T07:30:00Z" {
		t.Errorf("RangeBound datetime: %v %v", dt, err)
	}
	if _, err := RangeBound("bad", false); err == nil {
		t.Error("RangeBound(bad) must fail")
	}
}

func TestCleanTextAndList(t *testing.T) {
	if s, ok := CleanText("  привет  ", 1, 6); !ok || s != "привет" {
		t.Errorf("CleanText = %q %v", s, ok)
	}
	if _, ok := CleanText("   ", 1, 10); ok {
		t.Error("пустая строка должна быть отклонена")
	}
	if _, ok := CleanText("привет", 1, 5); ok {
		t.Error("длина считается в символах")
	}
	list, ok := CleanList([]string{" a ", "", "b", "a"}, 5, 10)
	if !ok || len(list) != 2 || list[0] != "a" || list[1] != "b" {
		t.Errorf("CleanList = %v %v", list, ok)
	}
	if l, ok := CleanList(nil, 5, 10); !ok || l == nil {
		t.Error("CleanList(nil) должен вернуть пустой не-nil срез")
	}
	if _, ok := CleanList([]string{"a", "b", "c"}, 2, 10); ok {
		t.Error("превышено количество элементов")
	}
}

func TestLikePattern(t *testing.T) {
	if got := LikePattern(`50%_a\b`); got != `%50\%\_a\\b%` {
		t.Errorf("LikePattern = %s", got)
	}
}

func TestRecurrenceDates(t *testing.T) {
	start := time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC)
	got, err := RecurrenceDates(start, "monthly", 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2026-01-31", "2026-02-28", "2026-03-31"}
	for i, w := range want {
		if got[i].Format(DateLayout) != w {
			t.Errorf("monthly[%d] = %s, want %s", i, got[i].Format(DateLayout), w)
		}
	}
	weekly, _ := RecurrenceDates(start, "weekly", 2)
	if weekly[1].Format(DateLayout) != "2026-02-07" {
		t.Errorf("weekly = %v", weekly)
	}
	daily, _ := RecurrenceDates(start, "daily", 52)
	if len(daily) != 52 {
		t.Errorf("daily len = %d", len(daily))
	}
	if _, err := RecurrenceDates(start, "daily", 53); err == nil {
		t.Error("count > 52 должен быть отклонён")
	}
	if _, err := RecurrenceDates(start, "yearly", 1); err == nil {
		t.Error("неизвестный recurrence должен быть отклонён")
	}
}

func TestTaskSortOrder(t *testing.T) {
	for _, s := range []string{"", "created_at", "-created_at", "due_date", "-due_date", "priority", "-updated_at"} {
		if _, ok := TaskSortOrder(s); !ok {
			t.Errorf("sort %q должен поддерживаться", s)
		}
	}
	if _, ok := TaskSortOrder("title; DROP TABLE tasks"); ok {
		t.Error("произвольный sort должен быть отклонён")
	}
}

func TestDurationMinutes(t *testing.T) {
	s := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	if DurationMinutes(s, s.Add(90*time.Minute+59*time.Second)) != 90 {
		t.Error("ожидалось 90 минут")
	}
	if DurationMinutes(s, s.Add(-time.Minute)) != 0 {
		t.Error("отрицательная длительность → 0")
	}
}

func TestOneOf(t *testing.T) {
	if !OneOf("todo", TaskStatuses) || OneOf("TODO", TaskStatuses) {
		t.Error("OneOf")
	}
}
