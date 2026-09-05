package storage

import (
	"context"
	"sort"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// Same statistics in Go. The tests run every case against both this and
// Postgres, so a mistake in the SQL turns up as a disagreement.

// LatestDraw is the newest day held.
func (m *Memory) LatestDraw(_ context.Context) (time.Time, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.latest()
}

func (m *Memory) latest() (time.Time, bool, error) {
	var newest time.Time
	for _, draw := range m.draws {
		if newest.IsZero() || draw.Date.After(newest) {
			newest = draw.Date
		}
	}
	return newest, !newest.IsZero(), nil
}

// days returns every stored day, oldest first.
func (m *Memory) days() []time.Time {
	out := make([]time.Time, 0, len(m.draws))
	for _, draw := range m.draws {
		out = append(out, draw.Date)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

// tailsOn returns the tails a day held, using every prize or just the special.
func (m *Memory) tailsOn(day time.Time, specialOnly bool) []string {
	draw, ok := m.draws[domain.FormatISO(day)]
	if !ok {
		return nil
	}
	if specialOnly {
		return []string{draw.Prizes.De()}
	}
	numbers := draw.Prizes.Numbers()
	out := make([]string, 0, len(numbers))
	for _, n := range numbers {
		out = append(out, domain.Lo(n))
	}
	return out
}

// LoGan ranks tails by how long they have gone unseen.
func (m *Memory) LoGan(_ context.Context, limit int) ([]domain.Gan, error) {
	return m.gan(false, limit), nil
}

// DeGan does the same for the special prize alone.
func (m *Memory) DeGan(_ context.Context, limit int) ([]domain.Gan, error) {
	return m.gan(true, limit), nil
}

func (m *Memory) gan(specialOnly bool, limit int) []domain.Gan {
	if limit <= 0 {
		limit = 12
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	newest, ok, _ := m.latest()
	if !ok {
		return nil
	}

	appearances := make(map[string][]time.Time)
	for _, day := range m.days() {
		seen := make(map[string]bool)
		for _, lo := range m.tailsOn(day, specialOnly) {
			if !seen[lo] {
				seen[lo] = true
				appearances[lo] = append(appearances[lo], day)
			}
		}
	}

	out := make([]domain.Gan, 0, len(appearances))
	for lo, seenOn := range appearances {
		g := domain.Gan{Number: lo, LastSeen: seenOn[len(seenOn)-1]}
		g.Days = daysBetween(g.LastSeen, newest)
		for i := 1; i < len(seenOn); i++ {
			// A gap of n days between appearances means n-1 days absent.
			if run := daysBetween(seenOn[i-1], seenOn[i]) - 1; run > g.Record {
				g.Record, g.RecordEnd = run, seenOn[i]
			}
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Days != out[j].Days {
			return out[i].Days > out[j].Days
		}
		return out[i].Number < out[j].Number
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Frequency counts appearances over the last days draws.
func (m *Memory) Frequency(_ context.Context, days int) ([]domain.Frequency, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	newest, ok, _ := m.latest()
	if !ok {
		return nil, nil
	}
	counts := make(map[string]*domain.Frequency)
	for _, day := range m.days() {
		if days > 0 && daysBetween(day, newest) >= days {
			continue
		}
		onThisDay := make(map[string]bool)
		for _, lo := range m.tailsOn(day, false) {
			entry, exists := counts[lo]
			if !exists {
				entry = &domain.Frequency{Number: lo}
				counts[lo] = entry
			}
			entry.Hits++
			if !onThisDay[lo] {
				onThisDay[lo] = true
				entry.Days++
			}
		}
	}
	out := make([]domain.Frequency, 0, len(counts))
	for _, entry := range counts {
		out = append(out, *entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Hits != out[j].Hits {
			return out[i].Hits > out[j].Hits
		}
		return out[i].Number < out[j].Number
	})
	return out, nil
}

// Profile gathers one number's history.
func (m *Memory) Profile(_ context.Context, lo string) (domain.Profile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	profile := domain.Profile{Number: lo, Gan: domain.Gan{Number: lo}, Archive: len(m.draws)}
	if profile.Archive == 0 {
		return profile, nil
	}
	newest, _, _ := m.latest()

	var seenOn []time.Time
	hitsOn := make(map[string]int)
	for _, day := range m.days() {
		hits := 0
		for _, tail := range m.tailsOn(day, false) {
			if tail == lo {
				hits++
			}
		}
		if hits > 0 {
			seenOn = append(seenOn, day)
			hitsOn[domain.FormatISO(day)] = hits
		}
	}

	if len(seenOn) > 0 {
		profile.First = seenOn[0]
		profile.Gan.LastSeen = seenOn[len(seenOn)-1]
		profile.Gan.Days = daysBetween(profile.Gan.LastSeen, newest)
		for i := 1; i < len(seenOn); i++ {
			if run := daysBetween(seenOn[i-1], seenOn[i]) - 1; run > profile.Gan.Record {
				profile.Gan.Record, profile.Gan.RecordEnd = run, seenOn[i]
			}
		}
		if len(seenOn) > 1 {
			span := daysBetween(seenOn[0], seenOn[len(seenOn)-1])
			profile.AvgCycle = float64(span) / float64(len(seenOn)-1)
		}
	}

	for _, w := range profileWindows {
		window := domain.Window{Label: w.Label, Days: w.Days}
		for _, day := range m.days() {
			if w.Days > 0 && daysBetween(day, newest) >= w.Days {
				continue
			}
			window.Total++
			if hits := hitsOn[domain.FormatISO(day)]; hits > 0 {
				window.Hits += hits
				window.Draws++
			}
		}
		profile.Windows = append(profile.Windows, window)
	}

	for _, day := range m.days() {
		if daysBetween(day, newest) >= domain.RecentDays {
			continue
		}
		profile.Recent = append(profile.Recent,
			domain.DayHit{Day: day, Hits: hitsOn[domain.FormatISO(day)]})
	}
	return profile, nil
}

// SpecialMonth lists one month of special prizes.
func (m *Memory) SpecialMonth(_ context.Context, year int, month time.Month) ([]domain.SpecialDay, error) {
	first, last := domain.MonthRange(year, month)
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []domain.SpecialDay
	for _, day := range m.days() {
		if day.Before(first) || day.After(last) {
			continue
		}
		draw := m.draws[domain.FormatISO(day)]
		special := draw.Prizes.Special()
		out = append(out, domain.SpecialDay{Day: day, Special: special, De: domain.Lo(special)})
	}
	return out, nil
}

// daysBetween counts whole calendar days, which is what "gan N ngày" means.
func daysBetween(from, to time.Time) int {
	return int(domain.DayOf(to).Sub(domain.DayOf(from)).Hours() / 24)
}
