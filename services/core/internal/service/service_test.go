package service_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/provider"
	"github.com/minhhdtr/xsmb-discord-bot/internal/service"
	"github.com/minhhdtr/xsmb-discord-bot/internal/storage"
)

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func drawFor(t *testing.T, day time.Time) domain.Draw {
	t.Helper()
	numbers := make([]string, 0, domain.TotalNumbers)
	for _, spec := range domain.PrizeLayout {
		for n := 0; n < spec.Count; n++ {
			numbers = append(numbers, fmt.Sprintf("%0*d", spec.Digits, n+1))
		}
	}
	prizes, err := domain.NewPrizes(numbers)
	if err != nil {
		t.Fatal(err)
	}
	return domain.Draw{Date: domain.DayOf(day), Prizes: prizes, Source: "fake",
		FetchedAt: time.Now().In(domain.Location())}
}

// fakeProvider returns scripted outcomes and counts calls.
type fakeProvider struct {
	mu       sync.Mutex
	calls    int32
	script   []domain.Outcome
	fallback func(day time.Time) domain.Outcome
	delay    time.Duration
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) Fetch(ctx context.Context, day time.Time) domain.Outcome {
	atomic.AddInt32(&f.calls, 1)
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return domain.Failed(ctx.Err())
		}
	}
	// Never run caller code while holding the lock, or every worker serialises
	// and the concurrency assertions mean nothing.
	f.mu.Lock()
	if len(f.script) > 0 {
		next := f.script[0]
		f.script = f.script[1:]
		f.mu.Unlock()
		return next
	}
	fallback := f.fallback
	f.mu.Unlock()

	if fallback != nil {
		return fallback(day)
	}
	return domain.Failed(errors.New("fake: no script left"))
}

func (f *fakeProvider) Calls() int { return int(atomic.LoadInt32(&f.calls)) }

func at(hour, minute int) time.Time {
	return time.Date(2026, 8, 21, hour, minute, 0, 0, domain.Location())
}

func newService(t *testing.T, src provider.Provider, clock func() time.Time) (*service.Service, *storage.Memory) {
	t.Helper()
	store := storage.NewMemory()
	return service.New(store, src, clock, quietLog()), store
}

func TestGetReadsFromStoreWithoutCrawling(t *testing.T) {
	day := domain.NewDate(2026, 8, 20)
	src := &fakeProvider{}
	svc, store := newService(t, src, func() time.Time { return at(20, 0) })
	if err := store.SaveDraw(context.Background(), drawFor(t, day)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(context.Background(), day); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if src.Calls() != 0 {
		t.Fatalf("crawled %d times for a stored day", src.Calls())
	}
}

func TestGetCrawlsThenCaches(t *testing.T) {
	day := domain.NewDate(2026, 8, 20)
	src := &fakeProvider{fallback: func(d time.Time) domain.Outcome { return domain.Found(drawFor(t, d)) }}
	svc, _ := newService(t, src, func() time.Time { return at(20, 0) })

	for i := 0; i < 3; i++ {
		if _, err := svc.Get(context.Background(), day); err != nil {
			t.Fatalf("Get %d: %v", i, err)
		}
	}
	if src.Calls() != 1 {
		t.Fatalf("crawled %d times, want 1", src.Calls())
	}
}

func TestLatestBefore1835ReturnsYesterday(t *testing.T) {
	var asked []string
	src := &fakeProvider{fallback: func(d time.Time) domain.Outcome {
		asked = append(asked, domain.FormatVN(d))
		return domain.Found(drawFor(t, d))
	}}
	svc, _ := newService(t, src, func() time.Time { return at(9, 0) })

	draw, err := svc.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got := domain.FormatVN(draw.Date); got != "20/08/2026" {
		t.Fatalf("got %s, want yesterday", got)
	}
	if len(asked) != 1 || asked[0] != "20/08/2026" {
		t.Fatalf("asked for %v", asked)
	}
}

func TestLatestAfter1835ReturnsToday(t *testing.T) {
	src := &fakeProvider{fallback: func(d time.Time) domain.Outcome { return domain.Found(drawFor(t, d)) }}
	svc, _ := newService(t, src, func() time.Time { return at(18, 36) })
	draw, err := svc.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got := domain.FormatVN(draw.Date); got != "21/08/2026" {
		t.Fatalf("got %s, want today", got)
	}
}

// A transient failure must not be remembered as "this day has no result".
func TestNetworkFailureIsNotCachedAsAbsence(t *testing.T) {
	day := domain.NewDate(2026, 8, 20)
	src := &fakeProvider{script: []domain.Outcome{
		domain.Failed(errors.New("connection reset")),
		domain.Found(drawFor(t, day)),
	}}
	svc, store := newService(t, src, func() time.Time { return at(20, 0) })
	ctx := context.Background()

	if _, err := svc.Get(ctx, day); err == nil {
		t.Fatal("first Get should have failed")
	}
	if absent, _ := store.IsAbsent(ctx, day); absent {
		t.Fatal("a network failure was recorded as an absence")
	}
	if _, err := svc.Get(ctx, day); err != nil {
		t.Fatalf("retry after a transient failure: %v", err)
	}
}

func TestConfirmed404OnAPastDayIsCachedAsAbsence(t *testing.T) {
	day := domain.NewDate(2011, 3, 7)
	src := &fakeProvider{fallback: func(time.Time) domain.Outcome { return domain.Absent() }}
	svc, store := newService(t, src, func() time.Time { return at(20, 0) })
	ctx := context.Background()

	if _, err := svc.Get(ctx, day); !errors.Is(err, service.ErrNoResult) {
		t.Fatalf("err = %v, want ErrNoResult", err)
	}
	if absent, _ := store.IsAbsent(ctx, day); !absent {
		t.Fatal("confirmed absence not cached")
	}
	if _, err := svc.Get(ctx, day); !errors.Is(err, service.ErrNoResult) {
		t.Fatalf("second Get: %v", err)
	}
	if src.Calls() != 1 {
		t.Fatalf("crawled %d times, want 1", src.Calls())
	}
}

// Before 18:35 the same 404 means "not out yet" and must not be cached.
func TestMissingTodayBefore1835IsNotYetAndNotCached(t *testing.T) {
	today := domain.NewDate(2026, 8, 21)
	src := &fakeProvider{fallback: func(time.Time) domain.Outcome { return domain.Absent() }}
	svc, store := newService(t, src, func() time.Time { return at(10, 0) })
	ctx := context.Background()

	_, err := svc.Get(ctx, today)
	if !errors.Is(err, service.ErrNotYet) {
		t.Fatalf("err = %v, want ErrNotYet", err)
	}
	if absent, _ := store.IsAbsent(ctx, today); absent {
		t.Fatal("today was cached as absent before 18:35")
	}
}

func TestPartialPageBefore1835IsNotYet(t *testing.T) {
	today := domain.NewDate(2026, 8, 21)
	src := &fakeProvider{fallback: func(time.Time) domain.Outcome {
		return domain.Failed(fmt.Errorf("parse: %w", domain.ErrIncomplete))
	}}
	svc, _ := newService(t, src, func() time.Time { return at(18, 20) })
	if _, err := svc.Get(context.Background(), today); !errors.Is(err, service.ErrNotYet) {
		t.Fatalf("err = %v, want ErrNotYet", err)
	}
}

func TestEmptyPageOnADueDayIsAbsence(t *testing.T) {
	day := domain.NewDate(2011, 3, 7)
	src := &fakeProvider{fallback: func(time.Time) domain.Outcome {
		return domain.Failed(fmt.Errorf("xoso: %w", provider.ErrNoData))
	}}
	svc, store := newService(t, src, func() time.Time { return at(20, 0) })
	if _, err := svc.Get(context.Background(), day); !errors.Is(err, service.ErrNoResult) {
		t.Fatalf("err = %v, want ErrNoResult", err)
	}
	if absent, _ := store.IsAbsent(context.Background(), day); !absent {
		t.Fatal("absence not cached")
	}
}

func TestBlockedIsSurfacedNotSwallowed(t *testing.T) {
	day := domain.NewDate(2026, 8, 20)
	src := &fakeProvider{fallback: func(time.Time) domain.Outcome {
		return domain.Failed(fmt.Errorf("xoso: %w", provider.ErrBlocked))
	}}
	svc, _ := newService(t, src, func() time.Time { return at(20, 0) })
	_, err := svc.Get(context.Background(), day)
	if !errors.Is(err, provider.ErrBlocked) {
		t.Fatalf("err = %v, want ErrBlocked", err)
	}
}

func TestOutOfRangeDatesNeverReachTheNetwork(t *testing.T) {
	src := &fakeProvider{}
	svc, _ := newService(t, src, func() time.Time { return at(20, 0) })
	for _, day := range []time.Time{
		domain.NewDate(1999, 1, 1),
		domain.NewDate(2026, 12, 25),
	} {
		if _, err := svc.Get(context.Background(), day); !errors.Is(err, domain.ErrOutOfRange) {
			t.Fatalf("%s: err = %v", domain.FormatVN(day), err)
		}
	}
	if src.Calls() != 0 {
		t.Fatalf("crawled %d times for out-of-range dates", src.Calls())
	}
}

// Twenty people typing !xsmb at 18:36 must produce one crawl, not twenty.
func TestConcurrentMissesShareOneCrawl(t *testing.T) {
	day := domain.NewDate(2026, 8, 20)
	src := &fakeProvider{
		delay:    40 * time.Millisecond,
		fallback: func(d time.Time) domain.Outcome { return domain.Found(drawFor(t, d)) },
	}
	svc, _ := newService(t, src, func() time.Time { return at(20, 0) })

	var wg sync.WaitGroup
	errs := make([]error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = svc.Get(context.Background(), day)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d: %v", i, err)
		}
	}
	if src.Calls() != 1 {
		t.Fatalf("crawled %d times, want 1", src.Calls())
	}
}
