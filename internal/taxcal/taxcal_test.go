package taxcal

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func find(t *testing.T, list []Deadline, titlePart string) Deadline {
	t.Helper()
	for _, d := range list {
		if strings.Contains(d.Title, titlePart) {
			return d
		}
	}
	t.Fatalf("нет срока с %q", titlePart)
	return Deadline{}
}

func ymd(d time.Time) string { return d.Format("2006-01-02") }

func TestShiftWeekend(t *testing.T) {
	cases := map[string]string{
		"2026-02-28": "2026-03-02", // суббота → понедельник
		"2027-03-28": "2027-03-29", // воскресенье → понедельник
		"2026-12-28": "2026-12-28", // понедельник
	}
	for in, want := range cases {
		d, _ := time.Parse("2006-01-02", in)
		if got := ymd(ShiftWeekend(d)); got != want {
			t.Errorf("ShiftWeekend(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestNPD(t *testing.T) {
	list, err := Deadlines(SelfEmployed, 2026)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 12 {
		t.Fatalf("НПД: %d сроков, ожидалось 12", len(list))
	}
	jan := find(t, list, "за январь 2026")
	if ymd(jan.Statutory) != "2026-02-28" || ymd(jan.Date) != "2026-03-02" {
		t.Errorf("НПД январь: %s → %s", ymd(jan.Statutory), ymd(jan.Date))
	}
	if !strings.Contains(jan.Description, "перенесён") {
		t.Error("в описании должен быть указан перенос")
	}
	dec := find(t, list, "за декабрь 2026")
	if ymd(dec.Statutory) != "2027-01-28" {
		t.Errorf("НПД декабрь: %s", ymd(dec.Statutory))
	}
	if list[0].Title != "Уплатить НПД за январь 2026" {
		t.Errorf("title = %q", list[0].Title)
	}
}

func TestUSN(t *testing.T) {
	ip, err := Deadlines(IP, 2026)
	if err != nil {
		t.Fatal(err)
	}
	ooo, err := Deadlines(OOO, 2026)
	if err != nil {
		t.Fatal(err)
	}
	if len(ip) != 9 || len(ooo) != 7 {
		t.Fatalf("ИП: %d (ожид. 9), ООО: %d (ожид. 7)", len(ip), len(ooo))
	}
	if d := find(t, ip, "налог по УСН за 2026"); ymd(d.Statutory) != "2027-04-28" {
		t.Errorf("ИП годовой УСН: %s", ymd(d.Statutory))
	}
	if d := find(t, ooo, "налог по УСН за 2026"); ymd(d.Statutory) != "2027-03-28" || ymd(d.Date) != "2027-03-29" {
		t.Errorf("ООО годовой УСН: %s → %s", ymd(d.Statutory), ymd(d.Date))
	}
	if d := find(t, ip, "уведомление об авансе по УСН за I квартал"); ymd(d.Statutory) != "2026-04-25" || ymd(d.Date) != "2026-04-27" {
		t.Errorf("уведомление Q1: %s → %s", ymd(d.Statutory), ymd(d.Date))
	}
	if d := find(t, ip, "аванс по УСН за 9 месяцев"); ymd(d.Statutory) != "2026-10-28" {
		t.Errorf("аванс 9 мес.: %s", ymd(d.Statutory))
	}
	if d := find(t, ip, "фиксированные"); ymd(d.Statutory) != "2026-12-28" {
		t.Errorf("фиксированные взносы: %s", ymd(d.Statutory))
	}
	if d := find(t, ip, "1% взносов"); ymd(d.Statutory) != "2027-07-01" {
		t.Errorf("1%%: %s", ymd(d.Statutory))
	}
	for _, d := range append(ip, ooo...) {
		if wd := d.Date.Weekday(); wd == time.Saturday || wd == time.Sunday {
			t.Errorf("%s выпадает на выходной: %s", d.Title, ymd(d.Date))
		}
		if !strings.Contains(d.Description, "праздничные дни") {
			t.Errorf("%s: в описании нет оговорки про праздники", d.Title)
		}
	}
	for _, d := range ooo {
		if strings.Contains(d.Title, "взнос") {
			t.Errorf("у ООО не должно быть взносов ИП: %s", d.Title)
		}
	}
}

func TestDeadlinesErrors(t *testing.T) {
	if _, err := Deadlines("llc", 2026); !errors.Is(err, ErrUnknownCompanyType) {
		t.Errorf("ожидалась ErrUnknownCompanyType, получено %v", err)
	}
	if _, err := Deadlines(IP, 1999); err == nil {
		t.Error("год вне диапазона должен быть отклонён")
	}
}
