package service_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/service"
)

type fakeGold struct {
	mu        sync.Mutex
	calls     int32
	histCalls int32
	buy       float64
	err       error
	delay     time.Duration
}

func (f *fakeGold) Name() string { return "fake-gold" }

func (f *fakeGold) Board(ctx context.Context) (domain.GoldBoard, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return domain.GoldBoard{}, ctx.Err()
		}
	}
	f.mu.Lock()
	err, buy := f.err, f.buy
	f.mu.Unlock()
	if err != nil {
		return domain.GoldBoard{}, err
	}
	return domain.NewGoldBoard([]domain.GoldQuote{{
		Code: "SJL1L10", Name: "Vàng miếng SJC",
		Buy: buy, Sell: buy + 3_000_000, Currency: domain.VND,
	}}, at(14, 0), "fake-gold", at(14, 0))
}

func (f *fakeGold) Calls() int { return int(atomic.LoadInt32(&f.calls)) }

func (f *fakeGold) History(ctx context.Context, code string, days int) (domain.GoldSeries, error) {
	atomic.AddInt32(&f.histCalls, 1)
	f.mu.Lock()
	err := f.err
	f.mu.Unlock()
	if err != nil {
		return domain.GoldSeries{}, err
	}
	points := make([]domain.GoldPoint, 0, days)
	for i := 0; i < days; i++ {
		points = append(points, domain.GoldPoint{
			Day:  domain.NewDate(2026, 8, 21).AddDate(0, 0, i-days+1),
			Buy:  143_000_000 + float64(i)*100_000,
			Sell: 146_000_000 + float64(i)*100_000,
		})
	}
	return domain.NewGoldSeries(code, code, domain.VND, points)
}

func (f *fakeGold) HistoryCalls() int { return int(atomic.LoadInt32(&f.histCalls)) }

func (f *fakeGold) set(buy float64, err error) {
	f.mu.Lock()
	f.buy, f.err = buy, err
	f.mu.Unlock()
}

func TestGoldCachesWithinTTL(t *testing.T) {
	src := &fakeGold{buy: 143_600_000}
	clock := at(14, 0)
	g := service.NewGold(src, time.Minute, time.Hour, func() time.Time { return clock }, quietLog())

	for i := 0; i < 5; i++ {
		if _, err := g.Board(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if src.Calls() != 1 {
		t.Fatalf("fetched %d times, want 1", src.Calls())
	}
}

func TestGoldRefreshesAfterTTL(t *testing.T) {
	src := &fakeGold{buy: 143_600_000}
	clock := at(14, 0)
	g := service.NewGold(src, time.Minute, time.Hour, func() time.Time { return clock }, quietLog())

	if _, err := g.Board(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(90 * time.Second)
	src.set(144_000_000, nil)

	board, err := g.Board(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if src.Calls() != 2 {
		t.Fatalf("fetched %d times, want 2", src.Calls())
	}
	if got := board.Quotes()[0].Buy; got != 144_000_000 {
		t.Fatalf("buy = %v, want the refreshed price", got)
	}
}

// A price from a few minutes ago beats an error message.
func TestGoldServesStaleWhenTheSourceFails(t *testing.T) {
	src := &fakeGold{buy: 143_600_000}
	clock := at(14, 0)
	g := service.NewGold(src, time.Minute, 30*time.Minute, func() time.Time { return clock }, quietLog())

	if _, err := g.Board(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(5 * time.Minute)
	src.set(0, errors.New("upstream down"))

	board, err := g.Board(context.Background())
	if err != nil {
		t.Fatalf("stale board was not served: %v", err)
	}
	if got := board.Quotes()[0].Buy; got != 143_600_000 {
		t.Fatalf("buy = %v", got)
	}
}

// Past the grace window the error must surface.
func TestGoldStopsServingStalePastGrace(t *testing.T) {
	src := &fakeGold{buy: 143_600_000}
	clock := at(14, 0)
	g := service.NewGold(src, time.Minute, 10*time.Minute, func() time.Time { return clock }, quietLog())

	if _, err := g.Board(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Hour)
	src.set(0, errors.New("upstream down"))

	if _, err := g.Board(context.Background()); err == nil {
		t.Fatal("a two-hour-old board was served as current")
	}
}

func TestGoldFailsWhenTheFirstFetchFails(t *testing.T) {
	src := &fakeGold{err: errors.New("upstream down")}
	g := service.NewGold(src, time.Minute, time.Hour, func() time.Time { return at(14, 0) }, quietLog())
	if _, err := g.Board(context.Background()); err == nil {
		t.Fatal("cold cache reported success")
	}
}

// A burst of commands must cause one request, not one per caller.
func TestGoldCollapsesConcurrentRefreshes(t *testing.T) {
	src := &fakeGold{buy: 143_600_000, delay: 40 * time.Millisecond}
	clock := at(14, 0)
	g := service.NewGold(src, time.Minute, time.Hour, func() time.Time { return clock }, quietLog())

	var wg sync.WaitGroup
	errs := make([]error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = g.Board(context.Background())
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d: %v", i, err)
		}
	}
	if src.Calls() != 1 {
		t.Fatalf("fetched %d times, want 1", src.Calls())
	}
}

// A closed day can't change, so history is memoised much longer.
func TestGoldHistoryIsCachedPerCodeAndRange(t *testing.T) {
	src := &fakeGold{buy: 143_600_000}
	clock := at(14, 0)
	g := service.NewGold(src, time.Minute, time.Hour, func() time.Time { return clock }, quietLog())
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		series, err := g.History(ctx, "SJL1L10", 30)
		if err != nil {
			t.Fatal(err)
		}
		if series.Len() != 30 {
			t.Fatalf("got %d points", series.Len())
		}
	}
	if src.HistoryCalls() != 1 {
		t.Fatalf("fetched history %d times, want 1", src.HistoryCalls())
	}

	// A different range is a different cache entry.
	if _, err := g.History(ctx, "SJL1L10", 7); err != nil {
		t.Fatal(err)
	}
	if src.HistoryCalls() != 2 {
		t.Fatalf("a 7-day request reused the 30-day cache")
	}
	// So is a different code.
	if _, err := g.History(ctx, "SJ9999", 30); err != nil {
		t.Fatal(err)
	}
	if src.HistoryCalls() != 3 {
		t.Fatalf("a different code reused the cache")
	}

	// Past the history TTL it refetches.
	clock = clock.Add(45 * time.Minute)
	if _, err := g.History(ctx, "SJL1L10", 30); err != nil {
		t.Fatal(err)
	}
	if src.HistoryCalls() != 4 {
		t.Fatalf("history was not refreshed after its TTL")
	}
}

func TestGoldHistoryServesStaleOnFailure(t *testing.T) {
	src := &fakeGold{buy: 143_600_000}
	clock := at(14, 0)
	g := service.NewGold(src, time.Minute, time.Hour, func() time.Time { return clock }, quietLog())
	ctx := context.Background()

	if _, err := g.History(ctx, "SJL1L10", 30); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Hour)
	src.set(0, errors.New("upstream down"))

	if _, err := g.History(ctx, "SJL1L10", 30); err != nil {
		t.Fatalf("stale history was not served: %v", err)
	}
	// With nothing cached at all, the error must surface.
	if _, err := g.History(ctx, "NEVERFETCHED", 30); err == nil {
		t.Fatal("cold history reported success")
	}
}
