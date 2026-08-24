// Package domain holds the lottery rules. No HTTP, no database, no Discord.
package domain

import (
	"errors"
	"fmt"
	"strings"
)

// PrizeGroups is the number of prize tiers in a Northern Vietnam draw.
const PrizeGroups = 8

// TotalNumbers is the sum of PrizeLayout counts. A draw is complete or it
// does not exist.
const TotalNumbers = 27

// PrizeSpec describes one prize tier.
type PrizeSpec struct {
	Code   string
	Label  string
	Count  int
	Digits int
}

// PrizeLayout never changes, so 27 numbers in this order fully determine a
// draw. That's why storage only persists a flat array.
var PrizeLayout = [PrizeGroups]PrizeSpec{
	{Code: "special", Label: "Đặc biệt", Count: 1, Digits: 5},
	{Code: "prize1", Label: "Giải nhất", Count: 1, Digits: 5},
	{Code: "prize2", Label: "Giải nhì", Count: 2, Digits: 5},
	{Code: "prize3", Label: "Giải ba", Count: 6, Digits: 5},
	{Code: "prize4", Label: "Giải tư", Count: 4, Digits: 4},
	{Code: "prize5", Label: "Giải năm", Count: 6, Digits: 4},
	{Code: "prize6", Label: "Giải sáu", Count: 3, Digits: 3},
	{Code: "prize7", Label: "Giải bảy", Count: 4, Digits: 2},
}

// ErrIncomplete means a result doesn't carry all 27 numbers. The announcer
// relies on it: the site publishes tiers progressively, so an early scrape
// fails here and gets retried.
var ErrIncomplete = errors.New("incomplete draw")

// Prizes holds the 27 numbers of one draw. The slice is unexported, so an
// invalid Prizes can't be built from outside this package.
type Prizes struct {
	numbers []string
}

// NewPrizes validates 27 numbers in PrizeLayout order. Copies the input so
// later mutation by the caller can't corrupt it.
func NewPrizes(numbers []string) (Prizes, error) {
	if len(numbers) != TotalNumbers {
		return Prizes{}, fmt.Errorf("%w: got %d numbers, want %d", ErrIncomplete, len(numbers), TotalNumbers)
	}
	frozen := make([]string, TotalNumbers)
	copy(frozen, numbers)

	at := 0
	for _, spec := range PrizeLayout {
		for n := 0; n < spec.Count; n++ {
			value := frozen[at]
			if len(value) != spec.Digits {
				return Prizes{}, fmt.Errorf("%s[%d] = %q: want %d digits", spec.Code, n, value, spec.Digits)
			}
			if !allDigits(value) {
				return Prizes{}, fmt.Errorf("%s[%d] = %q: not a number", spec.Code, n, value)
			}
			at++
		}
	}
	return Prizes{numbers: frozen}, nil
}

// NewPrizesByGroup takes numbers still grouped by tier, the shape a parser
// produces.
func NewPrizesByGroup(groups [PrizeGroups][]string) (Prizes, error) {
	flat := make([]string, 0, TotalNumbers)
	for i, spec := range PrizeLayout {
		if len(groups[i]) != spec.Count {
			return Prizes{}, fmt.Errorf("%w: %s has %d numbers, want %d",
				ErrIncomplete, spec.Code, len(groups[i]), spec.Count)
		}
		flat = append(flat, groups[i]...)
	}
	return NewPrizes(flat)
}

// Valid reports whether p came from a constructor, guarding the zero value.
func (p Prizes) Valid() bool { return len(p.numbers) == TotalNumbers }

// Numbers returns all 27 numbers in PrizeLayout order, as a copy.
func (p Prizes) Numbers() []string {
	out := make([]string, len(p.numbers))
	copy(out, p.numbers)
	return out
}

// Group returns the numbers of tier i (0 = special .. 7 = prize7).
func (p Prizes) Group(i int) []string {
	if !p.Valid() || i < 0 || i >= PrizeGroups {
		return nil
	}
	at := 0
	for g, spec := range PrizeLayout {
		if g == i {
			out := make([]string, spec.Count)
			copy(out, p.numbers[at:at+spec.Count])
			return out
		}
		at += spec.Count
	}
	return nil
}

// Special is the headline number, the one people check first.
func (p Prizes) Special() string {
	if !p.Valid() {
		return ""
	}
	return p.numbers[0]
}

// Tails returns the last two digits of every number, sorted.
func (p Prizes) Tails() []string {
	if !p.Valid() {
		return nil
	}
	out := make([]string, 0, TotalNumbers)
	for _, n := range p.numbers {
		out = append(out, n[len(n)-2:])
	}
	insertionSort(out)
	return out
}

// TailsByHead buckets tails under their first digit - the classic đầu đuôi
// table.
func (p Prizes) TailsByHead() [10][]string {
	var buckets [10][]string
	for _, tail := range p.Tails() {
		head := int(tail[0] - '0')
		buckets[head] = append(buckets[head], tail[1:])
	}
	return buckets
}

func insertionSort(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	return strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' }) < 0
}
