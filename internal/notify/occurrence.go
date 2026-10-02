package notify

import (
	"fmt"
	"time"
)

// Moscow — московское время, фиксированно UTC+3 (переходов на летнее время нет с 2014 г.).
// Не зависит от tzdata в образе.
var Moscow = time.FixedZone("MSK", 3*60*60)

// MaxRemindDaysBefore — верхняя граница remind_days_before (валидация в API).
const MaxRemindDaysBefore = 365

// civil приводит момент к календарной дате (полночь UTC) по его собственным компонентам
// год/месяц/день — так же, как FormatDate в API (значение колонки DATE драйвер отдаёт как полночь).
func civil(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// TodayMoscow — «сегодня» по Москве как календарная дата (полночь UTC).
func TodayMoscow(now time.Time) time.Time { return civil(now.In(Moscow)) }

// IsYearly — типы важных дат, повторяющиеся ежегодно (напоминание по годовщине).
func IsYearly(typ string) bool { return typ == "birthday" || typ == "holiday" }

func isLeap(year int) bool { return year%4 == 0 && (year%100 != 0 || year%400 == 0) }

// anniversary — годовщина даты d в году year; 29 февраля в невисокосный год → 28 февраля.
func anniversary(d time.Time, year int) time.Time {
	day := d.Day()
	if d.Month() == time.February && day == 29 && !isLeap(year) {
		day = 28
	}
	return time.Date(year, d.Month(), day, 0, 0, 0, 0, time.UTC)
}

// NextOccurrence — ближайшее наступление важной даты, начиная с today (календарные даты).
// Для birthday/holiday — годовщина в текущем году, а если она уже прошла — в следующем;
// для остальных типов — сама дата (может быть в прошлом).
func NextOccurrence(date time.Time, typ string, today time.Time) time.Time {
	date, today = civil(date), civil(today)
	if !IsYearly(typ) {
		return date
	}
	occ := anniversary(date, today.Year())
	if occ.Before(today) {
		occ = anniversary(date, today.Year()+1)
	}
	return occ
}

// DateReminderDue решает, нужно ли напоминание сегодня: due, если 0 ≤ (occurrence − today) ≤ remindDaysBefore.
// Возвращает дату наступления (ключ дедупликации) и число оставшихся дней.
func DateReminderDue(date time.Time, typ string, remindDaysBefore int, today time.Time) (occurrence time.Time, daysLeft int, due bool) {
	today = civil(today)
	occurrence = NextOccurrence(date, typ, today)
	// Обе даты — полночь UTC, без переходов времени: разница кратна суткам.
	daysLeft = int(occurrence.Sub(today) / (24 * time.Hour))
	due = daysLeft >= 0 && daysLeft <= remindDaysBefore
	return occurrence, daysLeft, due
}

// DateReminderMessage — текст уведомления: «15.10.2026 — через 3 дн.» или «15.10.2026 — сегодня».
func DateReminderMessage(occurrence time.Time, daysLeft int) string {
	d := occurrence.Format("02.01.2006")
	if daysLeft <= 0 {
		return d + " — сегодня"
	}
	return fmt.Sprintf("%s — через %d дн.", d, daysLeft)
}
