package service_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/service"
	"github.com/minhhdtr/xsmb-discord-bot/internal/storage"
)

// countingStore records how often each statistic is actually computed.
type countingStore struct {
	*storage.Memory
	loGan, freq, profile int
}

func (c *countingStore) LoGan(ctx context.Context, limit int) ([]domain.Gan, error) {
	c.loGan++
	return c.Memory.LoGan(ctx, limit)
}

func (c *countingStore) Frequency(ctx context.Context, days int) ([]domain.Frequency, error) {
	c.freq++
	return c.Memory.Frequency(ctx, days)
}

func (c *countingStore) Profile(ctx context.Context, lo string) (domain.Profile, error) {
	c.profile++
	return c.Memory.Profile(ctx, lo)
}

func statsService(t *testing.T) (*service.Service, *countingStore) {
	t.Helper()
	store := &countingStore{Memory: storage.NewMemory()}
	src := &fakeProvider{fallback: func(d time.Time) domain.Outcome { return domain.Found(drawFor(t, d)) }}
	return service.New(store, src, func() time.Time { return at(20, 0) }, quietLog()), store
}

func TestStatsAreCachedUntilANewDrawLands(t *testing.T) {
	svc, store := statsService(t)
	ctx := context.Background()

	if err := store.SaveDraw(ctx, drawFor(t, domain.NewDate(2026, 8, 20))); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := svc.LoGan(ctx, 12); err != nil {
			t.Fatal(err)
		}
	}
	if store.loGan != 1 {
		t.Fatalf("computed %d times, want 1", store.loGan)
	}

	// A new draw is the only thing that drops the cache.
	if err := store.SaveDraw(ctx, drawFor(t, domain.NewDate(2026, 8, 21))); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LoGan(ctx, 12); err != nil {
		t.Fatal(err)
	}
	if store.loGan != 2 {
		t.Fatalf("a new draw did not invalidate the cache: computed %d times", store.loGan)
	}
}

// Different arguments are different questions and must not share an entry.
func TestStatsCacheKeysOnArguments(t *testing.T) {
	svc, store := statsService(t)
	ctx := context.Background()
	if err := store.SaveDraw(ctx, drawFor(t, domain.NewDate(2026, 8, 20))); err != nil {
		t.Fatal(err)
	}

	for _, days := range []int{30, 30, 90, 90, 0} {
		if _, err := svc.Frequency(ctx, days); err != nil {
			t.Fatal(err)
		}
	}
	if store.freq != 3 {
		t.Fatalf("computed %d times, want 3 distinct windows", store.freq)
	}

	for _, lo := range []string{"88", "88", "07"} {
		if _, err := svc.Profile(ctx, lo); err != nil {
			t.Fatal(err)
		}
	}
	if store.profile != 2 {
		t.Fatalf("computed %d profiles, want 2", store.profile)
	}
}

func TestStatsOnAnEmptyArchive(t *testing.T) {
	svc, _ := statsService(t)
	ctx := context.Background()
	if gan, err := svc.LoGan(ctx, 12); err != nil || len(gan) != 0 {
		t.Fatalf("gan = %v, err = %v", gan, err)
	}
	if profile, err := svc.Profile(ctx, "88"); err != nil || profile.Archive != 0 {
		t.Fatalf("profile = %+v, err = %v", profile, err)
	}
	days, err := svc.SpecialMonth(ctx, 2026, time.August)
	if err != nil || len(days) != 0 {
		t.Fatalf("special month = %v, err = %v", days, err)
	}
}

// Different statistics must not queue behind each other.
func TestDistinctStatsComputeInParallel(t *testing.T) {
	store := &slowStore{Memory: storage.NewMemory(), delay: 60 * time.Millisecond}
	src := &fakeProvider{fallback: func(d time.Time) domain.Outcome { return domain.Found(drawFor(t, d)) }}
	svc := service.New(store, src, func() time.Time { return at(20, 0) }, quietLog())
	ctx := context.Background()
	if err := store.SaveDraw(ctx, drawFor(t, domain.NewDate(2026, 8, 20))); err != nil {
		t.Fatal(err)
	}

	const callers = 10
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := svc.Profile(ctx, fmt.Sprintf("%02d", i)); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	// Serialised, ten sixty-millisecond builds would take six hundred.
	if elapsed > 400*time.Millisecond {
		t.Fatalf("ten distinct statistics took %v; they are still serialised", elapsed)
	}
}

// Identical questions must still collapse into one computation.
func TestIdenticalStatsShareOneComputation(t *testing.T) {
	store := &slowStore{Memory: storage.NewMemory(), delay: 40 * time.Millisecond}
	src := &fakeProvider{fallback: func(d time.Time) domain.Outcome { return domain.Found(drawFor(t, d)) }}
	svc := service.New(store, src, func() time.Time { return at(20, 0) }, quietLog())
	ctx := context.Background()
	if err := store.SaveDraw(ctx, drawFor(t, domain.NewDate(2026, 8, 20))); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.Profile(ctx, "88"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := store.profileCalls(); n != 1 {
		t.Fatalf("computed %d times, want 1", n)
	}
}

// A failed computation must not be cached.
func TestFailedStatIsNotCached(t *testing.T) {
	store := &slowStore{Memory: storage.NewMemory()}
	src := &fakeProvider{fallback: func(d time.Time) domain.Outcome { return domain.Found(drawFor(t, d)) }}
	svc := service.New(store, src, func() time.Time { return at(20, 0) }, quietLog())
	ctx := context.Background()
	if err := store.SaveDraw(ctx, drawFor(t, domain.NewDate(2026, 8, 20))); err != nil {
		t.Fatal(err)
	}

	store.setErr(errors.New("database went away"))
	if _, err := svc.Profile(ctx, "88"); err == nil {
		t.Fatal("expected a failure")
	}
	store.setErr(nil)
	if _, err := svc.Profile(ctx, "88"); err != nil {
		t.Fatalf("a transient failure poisoned the key: %v", err)
	}
}

// slowStore makes each statistic take measurable time and can be made to fail.
type slowStore struct {
	*storage.Memory
	delay time.Duration

	mu    sync.Mutex
	calls int
	err   error
}

func (s *slowStore) Profile(ctx context.Context, lo string) (domain.Profile, error) {
	s.mu.Lock()
	s.calls++
	err := s.err
	s.mu.Unlock()
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	if err != nil {
		return domain.Profile{}, err
	}
	return s.Memory.Profile(ctx, lo)
}

func (s *slowStore) profileCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *slowStore) setErr(err error) {
	s.mu.Lock()
	s.err = err
	s.mu.Unlock()
}
