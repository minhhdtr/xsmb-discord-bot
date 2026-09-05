package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

func TestParseLo(t *testing.T) {
	good := map[string]string{"88": "88", "7": "07", "07": "07", "0": "00", " 88 ": "88"}
	for input, want := range good {
		got, err := domain.ParseLo(input)
		if err != nil || got != want {
			t.Fatalf("ParseLo(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, bad := range []string{"", "123", "8a", "-1", "1.5", "  "} {
		if got, err := domain.ParseLo(bad); !errors.Is(err, domain.ErrBadLo) {
			t.Fatalf("ParseLo(%q) = %q, %v; want ErrBadLo", bad, got, err)
		}
	}
}

func TestLoTakesTheLastTwoDigits(t *testing.T) {
	cases := map[string]string{"94533": "33", "0950": "50", "361": "61", "20": "20", "5": ""}
	for input, want := range cases {
		if got := domain.Lo(input); got != want {
			t.Fatalf("Lo(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestDeIsTheSpecialPrizeTail(t *testing.T) {
	numbers := make([]string, 0, domain.TotalNumbers)
	for _, spec := range domain.PrizeLayout {
		for n := 0; n < spec.Count; n++ {
			value := "1"
			for len(value) < spec.Digits {
				value = "0" + value
			}
			numbers = append(numbers, value)
		}
	}
	numbers[0] = "94533"
	prizes, err := domain.NewPrizes(numbers)
	if err != nil {
		t.Fatal(err)
	}
	if got := prizes.De(); got != "33" {
		t.Fatalf("De() = %q", got)
	}
	var zero domain.Prizes
	if zero.De() != "" {
		t.Fatal("zero value returned a đề")
	}
}

func TestParseMonth(t *testing.T) {
	now := domain.NewDate(2026, 8, 22)
	cases := map[string]string{
		"08/2026": "08/2026",
		"8/2026":  "08/2026",
		"2026-08": "08/2026",
		"08-2026": "08/2026",
		"08":      "08/2026", // bare month means the current year
		"12/25":   "12/2025",
	}
	for input, want := range cases {
		year, month, err := domain.ParseMonth(input, now)
		if err != nil {
			t.Fatalf("ParseMonth(%q): %v", input, err)
		}
		if got := domain.FormatMonth(year, month); got != want {
			t.Fatalf("ParseMonth(%q) = %s, want %s", input, got, want)
		}
	}
	for _, bad := range []string{"", "13/2026", "abc", "1/2/3/4", "08/2026/01"} {
		if _, _, err := domain.ParseMonth(bad, now); !errors.Is(err, domain.ErrBadMonth) {
			t.Fatalf("ParseMonth(%q) accepted", bad)
		}
	}
}

func TestMonthRangeCoversWholeMonths(t *testing.T) {
	cases := []struct {
		year        int
		month       time.Month
		first, last string
	}{
		{2026, time.August, "01/08/2026", "31/08/2026"},
		{2026, time.February, "01/02/2026", "28/02/2026"},
		{2024, time.February, "01/02/2024", "29/02/2024"}, // leap year
		{2026, time.December, "01/12/2026", "31/12/2026"},
	}
	for _, c := range cases {
		first, last := domain.MonthRange(c.year, c.month)
		if domain.FormatVN(first) != c.first || domain.FormatVN(last) != c.last {
			t.Fatalf("%s: got %s..%s, want %s..%s", domain.FormatMonth(c.year, c.month),
				domain.FormatVN(first), domain.FormatVN(last), c.first, c.last)
		}
	}
}

func TestGanNewRecord(t *testing.T) {
	if !(domain.Gan{Days: 30, Record: 25}).NewRecord() {
		t.Fatal("a longer run than the record did not report one")
	}
	if (domain.Gan{Days: 20, Record: 25}).NewRecord() {
		t.Fatal("a shorter run reported a record")
	}
	if (domain.Gan{Days: 25, Record: 25}).NewRecord() {
		t.Fatal("equalling the record is not passing it")
	}
}
