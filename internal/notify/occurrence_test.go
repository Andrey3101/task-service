package notify

import (
	"testing"
	"time"
)

func d(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestTodayMoscow(t *testing.T) {
	cases := []struct {
		now  time.Time
		want string
	}{
		{time.Date(2026, 10, 2, 20, 59, 0, 0, time.UTC), "2026-10-02"},
		{time.Date(2026, 10, 2, 21, 0, 0, 0, time.UTC), "2026-10-03"}, // 00:00 МСК
		{time.Date(2026, 12, 31, 22, 30, 0, 0, time.UTC), "2027-01-01"},
		{time.Date(2026, 10, 3, 1, 0, 0, 0, time.FixedZone("X", -10*3600)), "2026-10-03"}, // 11:00 UTC → 14:00 МСК
	}
	for _, c := range cases {
		got := TodayMoscow(c.now)
		if got.Format("2006-01-02") != c.want || got.Location() != time.UTC || got.Hour() != 0 {
			t.Errorf("TodayMoscow(%s) = %s, want %s", c.now, got, c.want)
		}
	}
}

func TestNextOccurrence(t *testing.T) {
	cases := []struct {
		name, date, typ, today, want string
	}{
		{"deadline — сама дата", "2026-10-10", "deadline", "2026-10-03", "2026-10-10"},
		{"payment в прошлом остаётся в прошлом", "2025-01-15", "payment", "2026-10-03", "2025-01-15"},
		{"other — не повторяется", "2020-10-05", "other", "2026-10-03", "2020-10-05"},
		{"birthday в текущем году", "1990-10-05", "birthday", "2026-10-03", "2026-10-05"},
		{"birthday сегодня", "1990-10-03", "birthday", "2026-10-03", "2026-10-03"},
		{"birthday прошёл — следующий год", "1990-03-01", "birthday", "2026-10-03", "2027-03-01"},
		{"holiday через Новый год", "2000-01-02", "holiday", "2026-12-30", "2027-01-02"},
		{"holiday 31 декабря", "2000-12-31", "holiday", "2026-12-31", "2026-12-31"},
		{"29.02 → 28.02 в невисокосный", "2000-02-29", "birthday", "2026-01-10", "2026-02-28"},
		{"29.02 в високосный год", "2000-02-29", "birthday", "2028-01-10", "2028-02-29"},
		{"29.02 прошёл → следующий високосный", "2000-02-29", "birthday", "2027-03-01", "2028-02-29"},
		{"28.02 прошёл в високосный → 28.02 следующего", "2000-02-28", "birthday", "2028-02-29", "2029-02-28"},
		{"годовщина будущей даты — по текущему году", "2030-11-01", "holiday", "2026-10-03", "2026-11-01"},
	}
	for _, c := range cases {
		got := NextOccurrence(d(c.date), c.typ, d(c.today))
		if got.Format("2006-01-02") != c.want {
			t.Errorf("%s: NextOccurrence(%s,%s,%s) = %s, want %s", c.name, c.date, c.typ, c.today, got.Format("2006-01-02"), c.want)
		}
	}
}

func TestDateReminderDue(t *testing.T) {
	cases := []struct {
		name, date, typ string
		remind          int
		today           string
		wantOcc         string
		wantDays        int
		wantDue         bool
	}{
		{"deadline в окне", "2026-10-06", "deadline", 3, "2026-10-03", "2026-10-06", 3, true},
		{"deadline за окном", "2026-10-07", "deadline", 3, "2026-10-03", "2026-10-07", 4, false},
		{"deadline прошёл", "2026-10-02", "deadline", 3, "2026-10-03", "2026-10-02", -1, false},
		{"remind=0 — в день даты", "2026-10-03", "payment", 0, "2026-10-03", "2026-10-03", 0, true},
		{"remind=0 — накануне не напоминаем", "2026-10-04", "payment", 0, "2026-10-03", "2026-10-04", 1, false},
		{"birthday remind=0 сегодня", "1985-10-03", "birthday", 0, "2026-10-03", "2026-10-03", 0, true},
		{"birthday через год", "1985-12-29", "birthday", 7, "2026-12-30", "2027-12-29", 364, false},
		{"holiday через Новый год в окне", "2000-01-02", "holiday", 5, "2026-12-30", "2027-01-02", 3, true},
		{"29.02 в невисокосный год → 28.02", "1996-02-29", "birthday", 1, "2027-02-27", "2027-02-28", 1, true},
		{"29.02 в високосный год", "1996-02-29", "birthday", 1, "2028-02-28", "2028-02-29", 1, true},
		{"29.02, окно через 1 марта в невисокосный", "1996-02-29", "birthday", 2, "2027-03-01", "2028-02-29", 365, false},
		{"отрицательный remind — никогда", "2026-10-03", "deadline", -1, "2026-10-03", "2026-10-03", 0, false},
		{"максимальное окно", "2027-10-03", "other", 365, "2026-10-03", "2027-10-03", 365, true},
	}
	for _, c := range cases {
		occ, days, due := DateReminderDue(d(c.date), c.typ, c.remind, d(c.today))
		if occ.Format("2006-01-02") != c.wantOcc || days != c.wantDays || due != c.wantDue {
			t.Errorf("%s: got (%s, %d, %v), want (%s, %d, %v)", c.name, occ.Format("2006-01-02"), days, due, c.wantOcc, c.wantDays, c.wantDue)
		}
	}
}

// Значение колонки DATE может прийти как полночь в произвольной зоне — дата берётся по компонентам.
func TestDateReminderDueNonUTCInput(t *testing.T) {
	msk := time.Date(2026, 10, 5, 0, 0, 0, 0, Moscow)
	occ, days, due := DateReminderDue(msk, "deadline", 2, time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC))
	if occ.Format("2006-01-02") != "2026-10-05" || days != 2 || !due {
		t.Errorf("got (%s, %d, %v)", occ, days, due)
	}
}

func TestDateReminderMessage(t *testing.T) {
	if got := DateReminderMessage(d("2026-10-06"), 3); got != "06.10.2026 — через 3 дн." {
		t.Errorf("через: %q", got)
	}
	if got := DateReminderMessage(d("2026-10-03"), 0); got != "03.10.2026 — сегодня" {
		t.Errorf("сегодня: %q", got)
	}
}
