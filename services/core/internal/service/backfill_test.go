package service_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/provider"
	"github.com/minhhdtr/xsmb-discord-bot/internal/service"
)

func fastOpts(from time.Time) service.BackfillOptions {
	return service.BackfillOptions{From: from, Concurrency: 1}
}

func TestBackfillFillsARange(t *testing.T) {
	src := &fakeProvider{fallback: func(d time.Time) domain.Outcome { return domain.Found(drawFor(t, d)) }}
	svc, store := newService(t, src, func() time.Time { return at(20, 0) })

	// 21/08 back to 17/08 inclusive: five days.
	report := svc.Backfill(context.Background(), fastOpts(domain.NewDate(2026, 8, 17)), nil)
	if report.Fetched != 5 || report.Scanned != 5 || report.Failed != 0 {
		t.Fatalf("report = %+v", report)
	}
	stats, _ := store.Stats(context.Background())
	if stats.Draws != 5 {
		t.Fatalf("stored %d draws", stats.Draws)
	}
	if domain.FormatVN(stats.Latest) != "21/08/2026" {
		t.Fatalf("latest = %s", domain.FormatVN(stats.Latest))
	}
}

// The second run should cost nothing: every day is already known.
func TestBackfillResumesWithoutRefetching(t *testing.T) {
	src := &fakeProvider{fallback: func(d time.Time) domain.Outcome { return domain.Found(drawFor(t, d)) }}
	svc, _ := newService(t, src, func() time.Time { return at(20, 0) })
	opts := fastOpts(domain.NewDate(2026, 8, 17))

	svc.Backfill(context.Background(), opts, nil)
	before := src.Calls()

	second := svc.Backfill(context.Background(), opts, nil)
	if second.Skipped != 5 || second.Fetched != 0 {
		t.Fatalf("second run = %+v", second)
	}
	if src.Calls() != before {
		t.Fatalf("re-crawled: %d calls, was %d", src.Calls(), before)
	}
}

func TestBackfillWalksNewestFirst(t *testing.T) {
	var mu sync.Mutex
	var order []string
	src := &fakeProvider{fallback: func(d time.Time) domain.Outcome {
		mu.Lock()
		order = append(order, domain.FormatVN(d))
		mu.Unlock()
		return domain.Found(drawFor(t, d))
	}}
	svc, _ := newService(t, src, func() time.Time { return at(20, 0) })
	svc.Backfill(context.Background(), fastOpts(domain.NewDate(2026, 8, 19)), nil)

	want := []string{"21/08/2026", "20/08/2026", "19/08/2026"}
	if len(order) != len(want) {
		t.Fatalf("order = %v", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestBackfillClampsToArchiveStart(t *testing.T) {
	src := &fakeProvider{fallback: func(d time.Time) domain.Outcome { return domain.Found(drawFor(t, d)) }}
	svc, _ := newService(t, src, func() time.Time {
		return time.Date(2005, 10, 3, 20, 0, 0, 0, domain.Location())
	})
	report := svc.Backfill(context.Background(),
		fastOpts(domain.NewDate(1999, 1, 1)), nil)
	// 01/10, 02/10, 03/10 - and nothing before FirstDraw.
	if report.Scanned != 3 {
		t.Fatalf("report = %+v", report)
	}
}

func TestBackfillHonoursExplicitTo(t *testing.T) {
	src := &fakeProvider{fallback: func(d time.Time) domain.Outcome { return domain.Found(drawFor(t, d)) }}
	svc, _ := newService(t, src, func() time.Time { return at(20, 0) })
	report := svc.Backfill(context.Background(), service.BackfillOptions{
		From:        domain.NewDate(2026, 8, 10),
		To:          domain.NewDate(2026, 8, 12),
		Concurrency: 1,
	}, nil)
	if report.Scanned != 3 || report.Fetched != 3 {
		t.Fatalf("report = %+v", report)
	}
}

func TestBackfillCountsAbsencesSeparately(t *testing.T) {
	src := &fakeProvider{fallback: func(d time.Time) domain.Outcome {
		if d.Day()%2 == 0 {
			return domain.Absent()
		}
		return domain.Found(drawFor(t, d))
	}}
	svc, _ := newService(t, src, func() time.Time { return at(20, 0) })
	report := svc.Backfill(context.Background(), fastOpts(domain.NewDate(2026, 8, 18)), nil)
	if report.Fetched+report.Absent != 4 || report.Absent == 0 || report.Failed != 0 {
		t.Fatalf("report = %+v", report)
	}
}

// A blocked crawler must stop at once, not grind through thousands of days.
func TestBackfillAbortsImmediatelyWhenBlocked(t *testing.T) {
	src := &fakeProvider{fallback: func(time.Time) domain.Outcome {
		return domain.Failed(fmt.Errorf("xoso: %w", provider.ErrBlocked))
	}}
	svc, _ := newService(t, src, func() time.Time { return at(20, 0) })
	report := svc.Backfill(context.Background(), fastOpts(domain.NewDate(2005, 10, 1)), nil)
	if !report.Aborted || !errors.Is(report.Err, provider.ErrBlocked) {
		t.Fatalf("report = %+v", report)
	}
	if src.Calls() != 1 {
		t.Fatalf("made %d requests after being blocked", src.Calls())
	}
}

func TestBackfillAbortsAfterConsecutiveFailures(t *testing.T) {
	src := &fakeProvider{fallback: func(time.Time) domain.Outcome {
		return domain.Failed(errors.New("connection reset"))
	}}
	svc, _ := newService(t, src, func() time.Time { return at(20, 0) })
	report := svc.Backfill(context.Background(), service.BackfillOptions{
		From: domain.NewDate(2005, 10, 1), Concurrency: 1, StopAfterFailures: 3,
	}, nil)
	if !report.Aborted || report.Failed != 3 {
		t.Fatalf("report = %+v", report)
	}
}

// An isolated failure must not abort the run, and must not be cached.
func TestBackfillSurvivesAnIsolatedFailure(t *testing.T) {
	src := &fakeProvider{fallback: func(d time.Time) domain.Outcome {
		if d.Day() == 20 {
			return domain.Failed(errors.New("timeout"))
		}
		return domain.Found(drawFor(t, d))
	}}
	svc, store := newService(t, src, func() time.Time { return at(20, 0) })
	report := svc.Backfill(context.Background(), fastOpts(domain.NewDate(2026, 8, 18)), nil)
	if report.Failed != 1 || report.Fetched != 3 || report.Aborted {
		t.Fatalf("report = %+v", report)
	}
	if absent, _ := store.IsAbsent(context.Background(), domain.NewDate(2026, 8, 20)); absent {
		t.Fatal("a failed day was cached as absent")
	}
}

func TestBackfillStopsWhenContextEnds(t *testing.T) {
	src := &fakeProvider{fallback: func(d time.Time) domain.Outcome { return domain.Found(drawFor(t, d)) }}
	svc, _ := newService(t, src, func() time.Time { return at(20, 0) })

	ctx, cancel := context.WithCancel(context.Background())
	report := svc.Backfill(ctx, service.BackfillOptions{
		From: domain.NewDate(2005, 10, 1), Concurrency: 1, Rate: 5 * time.Millisecond,
	}, func(day time.Time, r service.Report) {
		if r.Scanned >= 3 {
			cancel()
		}
	})
	if !report.Aborted {
		t.Fatalf("report = %+v", report)
	}
	if report.Scanned > 6 {
		t.Fatalf("kept going after cancellation: %+v", report)
	}
}

func TestBackfillReportsNothingForAnEmptyRange(t *testing.T) {
	src := &fakeProvider{}
	svc, _ := newService(t, src, func() time.Time { return at(10, 0) })
	// Before 18:35 the newest publishable day is 20/08.
	report := svc.Backfill(context.Background(), fastOpts(domain.NewDate(2026, 8, 21)), nil)
	if report.Scanned != 0 || src.Calls() != 0 {
		t.Fatalf("report = %+v calls = %d", report, src.Calls())
	}
}

// No pacing, several workers: every day exactly once, nothing lost to a race.
func TestBackfillIsCorrectUnderConcurrency(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]int{}
	src := &fakeProvider{
		delay: 2 * time.Millisecond,
		fallback: func(d time.Time) domain.Outcome {
			mu.Lock()
			seen[domain.FormatISO(d)]++
			mu.Unlock()
			return domain.Found(drawFor(t, d))
		},
	}
	svc, store := newService(t, src, func() time.Time { return at(20, 0) })

	from := domain.NewDate(2026, 5, 1) // 113 days back from 21/08
	report := svc.Backfill(context.Background(),
		service.BackfillOptions{From: from, Concurrency: 8}, nil)

	want := 113
	if report.Scanned != want || report.Fetched != want || report.Failed != 0 {
		t.Fatalf("report = %+v, want %d days", report, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != want {
		t.Fatalf("fetched %d distinct days, want %d", len(seen), want)
	}
	for day, times := range seen {
		if times != 1 {
			t.Fatalf("%s fetched %d times", day, times)
		}
	}
	stats, _ := store.Stats(context.Background())
	if stats.Draws != want {
		t.Fatalf("stored %d draws, want %d", stats.Draws, want)
	}
}

func TestBackfillConcurrencyActuallyOverlaps(t *testing.T) {
	var mu sync.Mutex
	active, peak := 0, 0
	src := &fakeProvider{fallback: func(d time.Time) domain.Outcome {
		mu.Lock()
		active++
		if active > peak {
			peak = active
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		return domain.Found(drawFor(t, d))
	}}
	svc, _ := newService(t, src, func() time.Time { return at(20, 0) })
	svc.Backfill(context.Background(),
		service.BackfillOptions{From: domain.NewDate(2026, 7, 1), Concurrency: 6}, nil)

	mu.Lock()
	defer mu.Unlock()
	if peak < 2 {
		t.Fatalf("peak concurrency was %d; requests never overlapped", peak)
	}
}

// A blocked source still stops the run at once, even with workers in flight.
func TestBackfillAbortsUnderConcurrencyWhenBlocked(t *testing.T) {
	src := &fakeProvider{fallback: func(time.Time) domain.Outcome {
		return domain.Failed(fmt.Errorf("xoso: %w", provider.ErrBlocked))
	}}
	svc, _ := newService(t, src, func() time.Time { return at(20, 0) })
	report := svc.Backfill(context.Background(),
		service.BackfillOptions{From: domain.NewDate(2005, 10, 1), Concurrency: 8}, nil)

	if !report.Aborted || !errors.Is(report.Err, provider.ErrBlocked) {
		t.Fatalf("report = %+v", report)
	}
	// At most one batch of in-flight workers, not thousands of days.
	if src.Calls() > 8 {
		t.Fatalf("made %d requests after being blocked", src.Calls())
	}
}
