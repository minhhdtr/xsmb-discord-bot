package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

func sample() []string {
	out := make([]string, 0, domain.TotalNumbers)
	for _, spec := range domain.PrizeLayout {
		for n := 0; n < spec.Count; n++ {
			out = append(out, pad(n+1, spec.Digits))
		}
	}
	return out
}

func pad(n, digits int) string {
	s := ""
	for v := n; ; v /= 10 {
		s = string(rune('0'+v%10)) + s
		if v < 10 {
			break
		}
	}
	return strings.Repeat("0", digits-len(s)) + s
}

func TestLayoutSumsTo27(t *testing.T) {
	total := 0
	for _, spec := range domain.PrizeLayout {
		total += spec.Count
	}
	if total != domain.TotalNumbers {
		t.Fatalf("layout sums to %d, want %d", total, domain.TotalNumbers)
	}
}

func TestNewPrizesAcceptsCompleteDraw(t *testing.T) {
	p, err := domain.NewPrizes(sample())
	if err != nil {
		t.Fatalf("NewPrizes: %v", err)
	}
	if !p.Valid() {
		t.Fatal("Valid() = false on a constructed value")
	}
	if got := len(p.Numbers()); got != 27 {
		t.Fatalf("got %d numbers, want 27", got)
	}
	if got := p.Special(); got != "00001" {
		t.Fatalf("Special() = %q", got)
	}
	if got := p.Group(7); len(got) != 4 || got[3] != "04" {
		t.Fatalf("Group(7) = %v", got)
	}
}

func TestNewPrizesRejectsShortDraw(t *testing.T) {
	for _, drop := range []int{1, 5, 26} {
		short := sample()
		short = append(short[:drop], short[drop+1:]...)
		if _, err := domain.NewPrizes(short); !errors.Is(err, domain.ErrIncomplete) {
			t.Fatalf("dropping index %d: err = %v, want ErrIncomplete", drop, err)
		}
	}
}

func TestNewPrizesRejectsWrongWidthAndNonDigits(t *testing.T) {
	tooShort := sample()
	tooShort[0] = "123"
	if _, err := domain.NewPrizes(tooShort); err == nil {
		t.Fatal("accepted a 3-digit special prize")
	}
	notNumber := sample()
	notNumber[26] = "ab"
	if _, err := domain.NewPrizes(notNumber); err == nil {
		t.Fatal("accepted a non-numeric prize")
	}
}

func TestZeroPrizesIsInvalidAndInert(t *testing.T) {
	var zero domain.Prizes
	if zero.Valid() {
		t.Fatal("zero value reports Valid()")
	}
	if zero.Special() != "" || zero.Numbers() != nil && len(zero.Numbers()) != 0 || zero.Tails() != nil {
		t.Fatal("zero value returned data")
	}
	if zero.Group(0) != nil {
		t.Fatal("zero value Group returned data")
	}
}

func TestNumbersReturnsACopy(t *testing.T) {
	p, _ := domain.NewPrizes(sample())
	got := p.Numbers()
	got[0] = "99999"
	if p.Special() == "99999" {
		t.Fatal("mutating the returned slice changed the Prizes")
	}
}

func TestConstructorCopiesInput(t *testing.T) {
	input := sample()
	p, _ := domain.NewPrizes(input)
	input[0] = "99999"
	if p.Special() == "99999" {
		t.Fatal("mutating the caller slice changed the Prizes")
	}
}

func TestNewPrizesByGroupRejectsWrongCount(t *testing.T) {
	var groups [domain.PrizeGroups][]string
	at := 0
	flat := sample()
	for i, spec := range domain.PrizeLayout {
		groups[i] = flat[at : at+spec.Count]
		at += spec.Count
	}
	if _, err := domain.NewPrizesByGroup(groups); err != nil {
		t.Fatalf("valid groups rejected: %v", err)
	}
	groups[3] = groups[3][:2]
	if _, err := domain.NewPrizesByGroup(groups); !errors.Is(err, domain.ErrIncomplete) {
		t.Fatalf("err = %v, want ErrIncomplete", err)
	}
}

func TestTailsSortedAndBucketed(t *testing.T) {
	p, _ := domain.NewPrizes(sample())
	tails := p.Tails()
	if len(tails) != 27 {
		t.Fatalf("got %d tails", len(tails))
	}
	for i := 1; i < len(tails); i++ {
		if tails[i] < tails[i-1] {
			t.Fatalf("tails not sorted at %d: %v", i, tails)
		}
	}
	buckets := p.TailsByHead()
	total := 0
	for _, b := range buckets {
		total += len(b)
	}
	if total != 27 {
		t.Fatalf("buckets hold %d tails, want 27", total)
	}
}

// --- dates ---

func at(hour, min int) time.Time {
	return time.Date(2026, 8, 21, hour, min, 0, 0, domain.Location())
}

func TestLatestPublishedFlipsAt1835(t *testing.T) {
	cases := []struct {
		hour, min int
		want      string
	}{
		{8, 0, "20/08/2026"},
		{18, 34, "20/08/2026"},
		{18, 35, "21/08/2026"},
		{23, 59, "21/08/2026"},
	}
	for _, c := range cases {
		got := domain.FormatVN(domain.LatestPublished(at(c.hour, c.min)))
		if got != c.want {
			t.Fatalf("at %02d:%02d got %s, want %s", c.hour, c.min, got, c.want)
		}
	}
}

func TestLatestPublishedIsTimezoneIndependent(t *testing.T) {
	// 12:00 UTC is 19:00 in Hanoi, so today's result is already out.
	utcNoon := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	if got := domain.FormatVN(domain.LatestPublished(utcNoon)); got != "21/08/2026" {
		t.Fatalf("got %s, want 21/08/2026", got)
	}
	// 10:00 UTC is 17:00 in Hanoi - not yet.
	utcTen := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	if got := domain.FormatVN(domain.LatestPublished(utcTen)); got != "20/08/2026" {
		t.Fatalf("got %s, want 20/08/2026", got)
	}
}

func TestParseDateAcceptsWhatPeopleType(t *testing.T) {
	now := at(12, 0)
	cases := map[string]string{
		"14/08/2026":   "14/08/2026",
		"14-08-2026":   "14/08/2026",
		"14.08.2026":   "14/08/2026",
		"4/8/2026":     "04/08/2026",
		"2026-08-14":   "14/08/2026",
		"14/08/26":     "14/08/2026",
		"14/08":        "14/08/2026",
		" 14/08/2026 ": "14/08/2026",
	}
	for input, want := range cases {
		got, err := domain.ParseDate(input, now)
		if err != nil {
			t.Fatalf("ParseDate(%q): %v", input, err)
		}
		if domain.FormatVN(got) != want {
			t.Fatalf("ParseDate(%q) = %s, want %s", input, domain.FormatVN(got), want)
		}
	}
}

func TestParseDateRejectsGarbage(t *testing.T) {
	now := at(12, 0)
	for _, input := range []string{"", "hôm qua", "32/08/2026", "14/13/2026", "1/2/3/4", "abc"} {
		if got, err := domain.ParseDate(input, now); err == nil {
			t.Fatalf("ParseDate(%q) accepted, got %s", input, domain.FormatVN(got))
		}
	}
}

func TestInRange(t *testing.T) {
	now := at(20, 0)
	if err := domain.InRange(domain.NewDate(2005, 9, 30), now); !errors.Is(err, domain.ErrOutOfRange) {
		t.Fatalf("pre-archive date accepted: %v", err)
	}
	if err := domain.InRange(domain.NewDate(2026, 8, 22), now); !errors.Is(err, domain.ErrOutOfRange) {
		t.Fatalf("future date accepted: %v", err)
	}
	if err := domain.InRange(domain.NewDate(2026, 8, 21), now); err != nil {
		t.Fatalf("today rejected: %v", err)
	}
	if err := domain.InRange(domain.FirstDraw, now); err != nil {
		t.Fatalf("FirstDraw rejected: %v", err)
	}
}

func TestWeekdayVN(t *testing.T) {
	if got := domain.WeekdayVN(domain.NewDate(2026, 8, 21)); got != "Thứ Sáu" {
		t.Fatalf("got %q", got)
	}
	if got := domain.WeekdayVN(domain.NewDate(2026, 8, 23)); got != "Chủ Nhật" {
		t.Fatalf("got %q", got)
	}
}

func TestOutcomeKeepsFailureDistinctFromAbsence(t *testing.T) {
	if domain.Absent().Status == domain.Failed(errors.New("boom")).Status {
		t.Fatal("Absent and Failed collapsed into one status")
	}
	if domain.Found(domain.Draw{}).Status != domain.StatusFound {
		t.Fatal("Found did not report StatusFound")
	}
}
