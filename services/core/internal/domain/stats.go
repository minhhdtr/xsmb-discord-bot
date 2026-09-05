package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// LoDigits is the width of a lô: the last two digits of a prize.
const LoDigits = 2

// ErrBadLo means a string is not a two-digit lô.
var ErrBadLo = errors.New("not a two-digit number")

// ParseLo normalises what a person types into a lô, accepting "7" as "07".
func ParseLo(input string) (string, error) {
	s := strings.TrimSpace(input)
	if s == "" || len(s) > LoDigits || !allDigits(s) {
		return "", fmt.Errorf("%q: %w", input, ErrBadLo)
	}
	return strings.Repeat("0", LoDigits-len(s)) + s, nil
}

// Lo returns the two-digit tail of one prize number.
func Lo(number string) string {
	if len(number) < LoDigits {
		return ""
	}
	return number[len(number)-LoDigits:]
}

// De is the tail of the special prize - one per draw, against 27 lô, so its
// droughts run about ten times longer.
func (p Prizes) De() string {
	if !p.Valid() {
		return ""
	}
	return Lo(p.numbers[0])
}

// Gan is how long a number has gone without appearing, in calendar days.
// Record is its longest such run in the archive.
type Gan struct {
	Number    string
	LastSeen  time.Time // zero when the number has never appeared
	Days      int
	Record    int
	RecordEnd time.Time // the day the record run was broken
}

// NewRecord reports whether the current run has passed the old record.
func (g Gan) NewRecord() bool { return g.Days > g.Record }

// Frequency over a window. Hits counts appearances, Days counts draws - two
// prizes in one draw can share a tail, which players call "hai nháy".
type Frequency struct {
	Number string
	Hits   int
	Days   int
}

// Window is one span of a number's profile.
type Window struct {
	Label string
	Days  int // 0 means the whole archive
	Hits  int
	Draws int // draws in the window that held the number
	Total int // draws in the window
}

// RecentDays is how many draws the recent strip covers.
const RecentDays = 30

// DayHit is one draw in the recent strip: how many times the number came up
// that day. Zero means it didn't.
type DayHit struct {
	Day  time.Time
	Hits int
}

// Profile is everything worth knowing about a single number.
type Profile struct {
	Number   string
	Gan      Gan
	Windows  []Window
	Recent   []DayHit // oldest first, one entry per draw in the window
	AvgCycle float64  // mean calendar days between appearances, 0 if unknown
	First    time.Time
	Archive  int // draws held in the archive
}

// SpecialDay is one day's special prize, for the month table.
type SpecialDay struct {
	Day     time.Time
	Special string
	De      string
}

// ErrBadMonth means a string is not a month.
var ErrBadMonth = errors.New("cannot read month")

// ParseMonth reads the forms a person types for a whole month: "08/2026",
// "8/2026", "2026-08". A bare "08" means that month of the current year.
func ParseMonth(input string, now time.Time) (year int, month time.Month, err error) {
	s := strings.TrimSpace(input)
	s = strings.NewReplacer(".", "/", "-", "/", " ", "/").Replace(s)
	for strings.Contains(s, "//") {
		s = strings.ReplaceAll(s, "//", "/")
	}
	if s == "" {
		return 0, 0, fmt.Errorf("%q: %w", input, ErrBadMonth)
	}

	parts := strings.Split(s, "/")
	switch len(parts) {
	case 1:
		parts = append(parts, fmt.Sprintf("%d", DayOf(now).Year()))
	case 2:
		if len(parts[0]) == 4 { // yyyy/mm
			parts[0], parts[1] = parts[1], parts[0]
		}
	default:
		return 0, 0, fmt.Errorf("%q: %w", input, ErrBadMonth)
	}
	if len(parts[1]) == 2 {
		parts[1] = "20" + parts[1]
	}

	parsed, parseErr := time.ParseInLocation("1/2006", strings.Join(parts, "/"), tz)
	if parseErr != nil {
		return 0, 0, fmt.Errorf("%q: %w", input, ErrBadMonth)
	}
	return parsed.Year(), parsed.Month(), nil
}

// MonthRange is the first and last day of a month, in the lottery timezone.
func MonthRange(year int, month time.Month) (first, last time.Time) {
	first = NewDate(year, month, 1)
	return first, first.AddDate(0, 1, -1)
}

// FormatMonth renders a month as mm/yyyy.
func FormatMonth(year int, month time.Month) string {
	return fmt.Sprintf("%02d/%04d", int(month), year)
}
