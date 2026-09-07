package domain

import (
	"errors"
	"fmt"
	"strings"
)

// Grouping folds the hundred lô into ten buckets. These are the four ways
// people read the same numbers: by first digit, by last digit, by the two
// digits added, and by either digit.
type Grouping int

// The groupings, in the order the help lists them.
const (
	ByHead Grouping = iota
	ByTail
	BySum
	ByTouch
)

// ErrBadGrouping means a string names no grouping.
var ErrBadGrouping = errors.New("not a grouping")

// groupingNames maps what a person types onto a grouping. Both the bare and
// the accented spelling, since a phone keyboard gives one and a slash choice
// the other.
var groupingNames = map[string]Grouping{
	"dau": ByHead, "đầu": ByHead,
	"duoi": ByTail, "đuôi": ByTail, "dit": ByTail, "đít": ByTail,
	"tong": BySum, "tổng": BySum,
	"cham": ByTouch, "chạm": ByTouch,
}

// ParseGrouping reads the name of a grouping.
func ParseGrouping(input string) (Grouping, error) {
	if g, ok := groupingNames[strings.ToLower(strings.TrimSpace(input))]; ok {
		return g, nil
	}
	return 0, fmt.Errorf("%q: %w", input, ErrBadGrouping)
}

// Overlaps reports whether one lô can land in more than one bucket. Only
// ByTouch does: 87 touches both 8 and 7, so the buckets add up to more than
// the number of lô drawn. Callers say so rather than letting the total look
// wrong.
func (g Grouping) Overlaps() bool { return g == ByTouch }

// buckets returns the digits one lô belongs to under this grouping. A kép
// touches a single digit, not that digit twice.
func (g Grouping) buckets(lo string) []int {
	if len(lo) != LoDigits {
		return nil
	}
	head, tail := int(lo[0]-'0'), int(lo[1]-'0')
	switch g {
	case ByHead:
		return []int{head}
	case ByTail:
		return []int{tail}
	case BySum:
		return []int{(head + tail) % 10}
	case ByTouch:
		if head == tail {
			return []int{head}
		}
		return []int{head, tail}
	}
	return nil
}

// GroupCount is one bucket of a grouped frequency, with the share of an even
// split for scale. Without Even a bucket is just a number nobody can read.
type GroupCount struct {
	Digit int
	Hits  int
}

// GroupedFrequency is the ten buckets plus what an even split would put in
// each, so a reader can see how ordinary a figure is.
type GroupedFrequency struct {
	By      Grouping
	Buckets [10]GroupCount
	Total   int     // placements counted; above the lô drawn when By.Overlaps()
	Even    float64 // Total / 10, the flat line the buckets vary around
}

// GroupFrequency folds per-number counts into ten buckets.
//
// Only Hits carries over. Frequency.Days cannot: two numbers in one bucket can
// land on the same draw, so adding their draw counts would count that draw
// twice. Hits are appearances and do add.
func GroupFrequency(freq []Frequency, by Grouping) GroupedFrequency {
	out := GroupedFrequency{By: by}
	for digit := range out.Buckets {
		out.Buckets[digit].Digit = digit
	}
	for _, f := range freq {
		for _, digit := range by.buckets(f.Number) {
			out.Buckets[digit].Hits += f.Hits
			out.Total += f.Hits
		}
	}
	out.Even = float64(out.Total) / 10
	return out
}

// Peak is the largest bucket, for scaling a bar.
func (g GroupedFrequency) Peak() int {
	best := 0
	for _, b := range g.Buckets {
		if b.Hits > best {
			best = b.Hits
		}
	}
	return best
}
