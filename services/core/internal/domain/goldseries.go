package domain

import (
	"errors"
	"fmt"
	"time"
)

// ErrNoHistory means a series carried too few points to mean anything.
var ErrNoHistory = errors.New("gold history is empty")

// MaxHistoryDays is the ceiling the upstream API documents.
const MaxHistoryDays = 30

// GoldPoint is one day's closing quote.
type GoldPoint struct {
	Day  time.Time
	Buy  float64
	Sell float64
}

// GoldSeries is daily quotes for one gold type, oldest first. The chart
// walks the slice directly, so the order is part of the type.
type GoldSeries struct {
	Code     string
	Name     string
	Currency Currency
	points   []GoldPoint
}

// NewGoldSeries sorts and de-duplicates by day, keeping the last value seen
// for a day.
func NewGoldSeries(code, name string, currency Currency, points []GoldPoint) (GoldSeries, error) {
	if code == "" {
		return GoldSeries{}, errors.New("series has no code")
	}
	if len(points) == 0 {
		return GoldSeries{}, ErrNoHistory
	}

	byDay := make(map[string]GoldPoint, len(points))
	order := make([]string, 0, len(points))
	for _, p := range points {
		if p.Buy <= 0 {
			return GoldSeries{}, fmt.Errorf("%s: point %s has buy %v", code, FormatVN(p.Day), p.Buy)
		}
		if currency == VND && p.Sell > 0 && p.Sell < p.Buy {
			return GoldSeries{}, fmt.Errorf("%s: point %s sells below buy", code, FormatVN(p.Day))
		}
		key := FormatISO(p.Day)
		if _, seen := byDay[key]; !seen {
			order = append(order, key)
		}
		p.Day = DayOf(p.Day)
		byDay[key] = p
	}

	insertionSort(order)
	frozen := make([]GoldPoint, 0, len(order))
	for _, key := range order {
		frozen = append(frozen, byDay[key])
	}
	if name == "" {
		name = code
	}
	return GoldSeries{Code: code, Name: name, Currency: currency, points: frozen}, nil
}

// Valid reports whether the series came from the constructor.
func (s GoldSeries) Valid() bool { return len(s.points) > 0 }

// Points returns the series, oldest first, as a copy.
func (s GoldSeries) Points() []GoldPoint {
	out := make([]GoldPoint, len(s.points))
	copy(out, s.points)
	return out
}

// Len is the number of days held.
func (s GoldSeries) Len() int { return len(s.points) }

// First and Last are the ends of the series.
func (s GoldSeries) First() GoldPoint {
	if !s.Valid() {
		return GoldPoint{}
	}
	return s.points[0]
}

// Last returns the most recent point.
func (s GoldSeries) Last() GoldPoint {
	if !s.Valid() {
		return GoldPoint{}
	}
	return s.points[len(s.points)-1]
}

// Range is the lowest and highest value across both sides of the series.
func (s GoldSeries) Range() (low, high float64) {
	if !s.Valid() {
		return 0, 0
	}
	low, high = s.points[0].Buy, s.points[0].Buy
	for _, p := range s.points {
		for _, v := range []float64{p.Buy, p.Sell} {
			if v <= 0 {
				continue
			}
			if v < low {
				low = v
			}
			if v > high {
				high = v
			}
		}
	}
	return low, high
}

// Change is the movement of the buy side from the first day to the last.
func (s GoldSeries) Change() float64 {
	if s.Len() < 2 {
		return 0
	}
	return s.Last().Buy - s.First().Buy
}

// ChangePercent is Change relative to the starting price.
func (s GoldSeries) ChangePercent() float64 {
	if s.Len() < 2 || s.First().Buy == 0 {
		return 0
	}
	return s.Change() / s.First().Buy * 100
}

// TwoSided reports whether every point has a sell price. The world series
// doesn't.
func (s GoldSeries) TwoSided() bool {
	for _, p := range s.points {
		if p.Sell <= 0 {
			return false
		}
	}
	return s.Valid()
}
