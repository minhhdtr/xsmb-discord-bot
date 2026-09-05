package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	// Embeds the IANA database in the binary so Asia/Ho_Chi_Minh resolves even
	// in a scratch container with no tzdata package installed.
	_ "time/tzdata"
)

// tz is the only clock that matters. "Today" is defined by it regardless of
// where the host runs.
var tz = mustLoad("Asia/Ho_Chi_Minh")

// Location returns the lottery's timezone.
func Location() *time.Location { return tz }

// When a draw has published all 27 numbers. Tiers start appearing around
// 18:15; the last is out by 18:35.
const (
	CompleteHour   = 18
	CompleteMinute = 35
)

// FirstDraw bounds the archive. Older dates are rejected before any network
// call.
var FirstDraw = NewDate(2005, 10, 1)

// ErrOutOfRange means the requested date is before FirstDraw or in the future.
var ErrOutOfRange = errors.New("date out of range")

// NewDate builds midnight of a calendar day in the lottery timezone.
func NewDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, tz)
}

// DayOf truncates any instant to the calendar day it falls on locally.
func DayOf(t time.Time) time.Time {
	local := t.In(tz)
	return NewDate(local.Year(), local.Month(), local.Day())
}

// CompletionTime is the instant on day d when all 27 numbers are expected.
func CompletionTime(day time.Time) time.Time {
	d := DayOf(day)
	return time.Date(d.Year(), d.Month(), d.Day(), CompleteHour, CompleteMinute, 0, 0, tz)
}

// LatestPublished is the newest day whose result should exist by now. Before
// 18:35 that's yesterday, so a morning !xsmb answers instead of erroring.
func LatestPublished(now time.Time) time.Time {
	today := DayOf(now)
	if now.In(tz).Before(CompletionTime(today)) {
		return today.AddDate(0, 0, -1)
	}
	return today
}

// InRange reports whether a day can plausibly have a result yet.
func InRange(day, now time.Time) error {
	d := DayOf(day)
	if d.Before(FirstDraw) {
		return fmt.Errorf("%w: %s is before %s", ErrOutOfRange, FormatVN(d), FormatVN(FirstDraw))
	}
	if d.After(DayOf(now)) {
		return fmt.Errorf("%w: %s is in the future", ErrOutOfRange, FormatVN(d))
	}
	return nil
}

// ParseDate accepts what people actually type. Day before month, except for
// the unambiguous ISO form.
func ParseDate(input string, now time.Time) (time.Time, error) {
	s := strings.TrimSpace(input)
	s = strings.NewReplacer(".", "/", "-", "/", " ", "/").Replace(s)
	for strings.Contains(s, "//") {
		s = strings.ReplaceAll(s, "//", "/")
	}
	if s == "" {
		return time.Time{}, errors.New("empty date")
	}

	parts := strings.Split(s, "/")
	switch len(parts) {
	case 2: // dd/mm - assume the current year
		parts = append(parts, fmt.Sprintf("%d", DayOf(now).Year()))
	case 3:
		if len(parts[0]) == 4 { // yyyy/mm/dd - flip to dd/mm/yyyy
			parts[0], parts[2] = parts[2], parts[0]
		}
	default:
		return time.Time{}, fmt.Errorf("cannot read date %q", input)
	}

	if len(parts[2]) == 2 { // dd/mm/yy
		parts[2] = "20" + parts[2]
	}
	layout := "2/1/2006"
	parsed, err := time.ParseInLocation(layout, strings.Join(parts, "/"), tz)
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot read date %q", input)
	}
	return parsed, nil
}

// FormatVN renders a day as dd/mm/yyyy.
func FormatVN(day time.Time) string { return DayOf(day).Format("02/01/2006") }

// FormatISO renders a day as yyyy-mm-dd, the form used in URLs and SQL.
func FormatISO(day time.Time) string { return DayOf(day).Format("2006-01-02") }

var weekdays = [7]string{"Chủ Nhật", "Thứ Hai", "Thứ Ba", "Thứ Tư", "Thứ Năm", "Thứ Sáu", "Thứ Bảy"}

// WeekdayVN names the weekday in Vietnamese.
func WeekdayVN(day time.Time) string { return weekdays[int(DayOf(day).Weekday())] }

// Draw is one complete result.
type Draw struct {
	Date      time.Time
	Prizes    Prizes
	Source    string
	FetchedAt time.Time
}

// Status distinguishes the three things a fetch can mean.
type Status int

const (
	// StatusFound: the day has a complete result.
	StatusFound Status = iota
	// StatusAbsent: the source confirmed there is no result for this day.
	StatusAbsent
	// StatusFailed: we couldn't find out. Never cache this as Absent.
	StatusFailed
)

// Outcome is the three-way result of a fetch.
type Outcome struct {
	Status Status
	Draw   Draw
	Err    error
}

// Found wraps a complete draw.
func Found(d Draw) Outcome { return Outcome{Status: StatusFound, Draw: d} }

// Absent reports a day the source says has no result.
func Absent() Outcome { return Outcome{Status: StatusAbsent} }

// Failed reports that the question could not be answered.
func Failed(err error) Outcome { return Outcome{Status: StatusFailed, Err: err} }

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic("domain: cannot load timezone " + name + ": " + err.Error())
	}
	return loc
}
