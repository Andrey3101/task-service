// Package taxcal рассчитывает сроки налоговых платежей и уведомлений на год
// для самозанятых (НПД), ИП и ООО на УСН.
//
// Правила (НК РФ, редакция с 2023 г., ЕНП):
//   - НПД: налог за месяц уплачивается до 28 числа следующего месяца;
//   - УСН: уведомления об исчисленных авансах — до 25 апреля, 25 июля, 25 октября;
//     авансы — до 28 апреля, 28 июля, 28 октября; налог за год — до 28 апреля (ИП)
//     или 28 марта (ООО) следующего года;
//   - ИП: фиксированные страховые взносы — до 28 декабря, 1% с дохода свыше 300 000 ₽ —
//     до 1 июля следующего года.
//
// Срок, выпавший на субботу или воскресенье, переносится на следующий понедельник.
// Праздничные и перенесённые рабочие дни не учитываются.
package taxcal

import (
	"errors"
	"fmt"
	"time"
)

// Типы компаний из JWT (claim company_type).
const (
	SelfEmployed = "self_employed"
	IP           = "ip"
	OOO          = "ooo"
)

// ErrUnknownCompanyType — тип компании не поддерживается.
var ErrUnknownCompanyType = errors.New("неизвестный тип компании")

// Deadline — один срок.
type Deadline struct {
	Title       string
	Description string
	Date        time.Time // полночь UTC фактической даты (после переноса с выходных)
	Statutory   time.Time // дата по закону до переноса
}

var monthsNominative = [...]string{
	"январь", "февраль", "март", "апрель", "май", "июнь",
	"июль", "август", "сентябрь", "октябрь", "ноябрь", "декабрь",
}

var monthsGenitive = [...]string{
	"января", "февраля", "марта", "апреля", "мая", "июня",
	"июля", "августа", "сентября", "октября", "ноября", "декабря",
}

// ShiftWeekend переносит субботу/воскресенье на следующий понедельник.
func ShiftWeekend(d time.Time) time.Time {
	switch d.Weekday() {
	case time.Saturday:
		return d.AddDate(0, 0, 2)
	case time.Sunday:
		return d.AddDate(0, 0, 1)
	}
	return d
}

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func humanDate(d time.Time) string {
	return fmt.Sprintf("%d %s %d", d.Day(), monthsGenitive[d.Month()-1], d.Year())
}

func newDeadline(title, what string, statutory time.Time) Deadline {
	actual := ShiftWeekend(statutory)
	desc := fmt.Sprintf("%s. Срок по НК РФ — %s.", what, humanDate(statutory))
	if !actual.Equal(statutory) {
		desc += fmt.Sprintf(" Дата выпадает на выходной, срок перенесён на %s.", humanDate(actual))
	}
	desc += " Переносы с выходных учтены, праздничные дни — нет: сверьтесь с производственным календарём."
	return Deadline{Title: title, Description: desc, Date: actual, Statutory: statutory}
}

// Deadlines возвращает сроки на год year для типа компании companyType.
func Deadlines(companyType string, year int) ([]Deadline, error) {
	if year < 2000 || year > 2100 {
		return nil, errors.New("год должен быть в диапазоне 2000–2100")
	}
	switch companyType {
	case SelfEmployed:
		return npd(year), nil
	case IP:
		return usn(year, true), nil
	case OOO:
		return usn(year, false), nil
	}
	return nil, ErrUnknownCompanyType
}

func npd(year int) []Deadline {
	out := make([]Deadline, 0, 12)
	for m := time.January; m <= time.December; m++ {
		// 28 число следующего месяца; time.Date нормализует 13-й месяц в январь следующего года.
		due := date(year, m+1, 28)
		title := fmt.Sprintf("Уплатить НПД за %s %d", monthsNominative[m-1], year)
		what := fmt.Sprintf("Налог на профессиональный доход за %s %d г. (сумма — в приложении «Мой налог»)", monthsNominative[m-1], year)
		out = append(out, newDeadline(title, what, due))
	}
	return out
}

func usn(year int, isIP bool) []Deadline {
	periods := []struct {
		name  string
		month time.Month
	}{
		{"I квартал", time.April},
		{"полугодие", time.July},
		{"9 месяцев", time.October},
	}
	out := make([]Deadline, 0, 9)
	for _, p := range periods {
		out = append(out,
			newDeadline(
				fmt.Sprintf("Подать уведомление об авансе по УСН за %s %d", p.name, year),
				fmt.Sprintf("Уведомление об исчисленной сумме авансового платежа по УСН за %s %d г.", p.name, year),
				date(year, p.month, 25),
			),
			newDeadline(
				fmt.Sprintf("Уплатить аванс по УСН за %s %d", p.name, year),
				fmt.Sprintf("Авансовый платёж по УСН за %s %d г. (на ЕНС)", p.name, year),
				date(year, p.month, 28),
			),
		)
	}
	annualMonth := time.March
	if isIP {
		annualMonth = time.April
	}
	out = append(out, newDeadline(
		fmt.Sprintf("Уплатить налог по УСН за %d год", year),
		fmt.Sprintf("Налог по УСН за %d год (на ЕНС)", year),
		date(year+1, annualMonth, 28),
	))
	if isIP {
		out = append(out,
			newDeadline(
				fmt.Sprintf("Уплатить фиксированные страховые взносы ИП за %d год", year),
				fmt.Sprintf("Фиксированные страховые взносы ИП «за себя» за %d г.", year),
				date(year, time.December, 28),
			),
			newDeadline(
				fmt.Sprintf("Уплатить 1%% взносов с дохода свыше 300 000 ₽ за %d год", year),
				fmt.Sprintf("Страховые взносы ИП 1%% с дохода, превышающего 300 000 ₽, за %d г.", year),
				date(year+1, time.July, 1),
			),
		)
	}
	return out
}
