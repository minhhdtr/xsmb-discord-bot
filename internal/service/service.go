// Package service answers the two questions the bot asks: what came out on
// day X, and wait until today is complete.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/provider"
	"github.com/minhhdtr/xsmb-discord-bot/internal/storage"
)

var (
	// ErrNoResult means the day genuinely has no draw.
	ErrNoResult = errors.New("no result for this day")

	// ErrNotYet means the draw hasn't finished publishing. Temporary, and never
	// cached.
	ErrNotYet = errors.New("result not published yet")
)

// Service reads draws, crawling on demand and caching what it learns.
type Service struct {
	store storage.Store
	src   provider.Provider
	now   func() time.Time
	log   *slog.Logger

	mu       sync.Mutex
	inFlight map[string]*flight

	// archiveGen counts every write to the archive. Backfill fills gaps
	// behind the newest day, so max(draw_date) alone cannot tell the
	// statistics cache that the archive moved under it.
	archiveGen atomic.Uint64

	stats statsCache
}

// archiveChanged reports the current archive generation, for the stats cache.
func (s *Service) archiveChanged() uint64 { return s.archiveGen.Load() }

// flight lets concurrent requests for the same day share one crawl.
type flight struct {
	done chan struct{}
	draw domain.Draw
	err  error
}

// New builds a Service. clock is injectable so tests can sit either side of
// 18:35 without waiting.
func New(store storage.Store, src provider.Provider, clock func() time.Time, log *slog.Logger) *Service {
	if clock == nil {
		clock = func() time.Time { return time.Now().In(domain.Location()) }
	}
	if log == nil {
		log = slog.Default()
	}
	return &Service{store: store, src: src, now: clock, log: log, inFlight: make(map[string]*flight)}
}

// Now exposes the service clock so callers share one notion of time.
func (s *Service) Now() time.Time { return s.now().In(domain.Location()) }

// Latest returns the newest day whose result should be out.
func (s *Service) Latest(ctx context.Context) (domain.Draw, error) {
	return s.Get(ctx, domain.LatestPublished(s.Now()))
}

// Get returns one day's result, crawling if it is not stored yet.
func (s *Service) Get(ctx context.Context, day time.Time) (domain.Draw, error) {
	day = domain.DayOf(day)
	if err := domain.InRange(day, s.Now()); err != nil {
		return domain.Draw{}, err
	}
	if draw, found, err := s.store.Draw(ctx, day); err != nil {
		return domain.Draw{}, err
	} else if found {
		return draw, nil
	}
	if absent, err := s.store.IsAbsent(ctx, day); err != nil {
		return domain.Draw{}, err
	} else if absent {
		return domain.Draw{}, fmt.Errorf("%s: %w", domain.FormatVN(day), ErrNoResult)
	}
	return s.fetchOnce(ctx, day)
}

// fetchOnce collapses concurrent misses for the same day into a single crawl.
func (s *Service) fetchOnce(ctx context.Context, day time.Time) (domain.Draw, error) {
	key := domain.FormatISO(day)

	s.mu.Lock()
	if existing, running := s.inFlight[key]; running {
		s.mu.Unlock()
		select {
		case <-existing.done:
			return existing.draw, existing.err
		case <-ctx.Done():
			return domain.Draw{}, ctx.Err()
		}
	}
	current := &flight{done: make(chan struct{})}
	s.inFlight[key] = current
	s.mu.Unlock()

	current.draw, current.err = s.crawl(ctx, day)

	s.mu.Lock()
	delete(s.inFlight, key)
	s.mu.Unlock()
	close(current.done)

	return current.draw, current.err
}

// crawl decides what an outcome means. The only place that maps a provider
// result onto the clock, and the only place allowed to record an absence.
func (s *Service) crawl(ctx context.Context, day time.Time) (domain.Draw, error) {
	// Another flight may have stored the day while this one waited on the lock.
	if draw, found, err := s.store.Draw(ctx, day); err == nil && found {
		return draw, nil
	}

	outcome := s.src.Fetch(ctx, day)
	// Before 18:35 a missing result is just early news, not an absence.
	due := !s.Now().Before(domain.CompletionTime(day))

	switch outcome.Status {
	case domain.StatusFound:
		if err := s.store.SaveDraw(ctx, outcome.Draw); err != nil {
			// Serving a good result matters more than caching it.
			s.log.Error("cannot store draw", "date", domain.FormatISO(day), "error", err)
		} else {
			s.archiveGen.Add(1)
		}
		return outcome.Draw, nil

	case domain.StatusAbsent:
		if !due {
			return domain.Draw{}, fmt.Errorf("%s: %w", domain.FormatVN(day), ErrNotYet)
		}
		if err := s.store.MarkAbsent(ctx, day); err != nil {
			s.log.Error("cannot mark absence", "date", domain.FormatISO(day), "error", err)
		} else {
			s.archiveGen.Add(1)
		}
		return domain.Draw{}, fmt.Errorf("%s: %w", domain.FormatVN(day), ErrNoResult)

	default:
		if errors.Is(outcome.Err, provider.ErrNoData) && due {
			// The page exists and is due, but holds nothing. Treat as absent.
			if err := s.store.MarkAbsent(ctx, day); err != nil {
				s.log.Error("cannot mark absence", "date", domain.FormatISO(day), "error", err)
			} else {
				s.archiveGen.Add(1)
			}
			return domain.Draw{}, fmt.Errorf("%s: %w", domain.FormatVN(day), ErrNoResult)
		}
		if !due && (errors.Is(outcome.Err, provider.ErrNoData) || errors.Is(outcome.Err, domain.ErrIncomplete)) {
			return domain.Draw{}, fmt.Errorf("%s: %w", domain.FormatVN(day), ErrNotYet)
		}
		return domain.Draw{}, outcome.Err
	}
}

// AwaitComplete polls until the day is complete or ctx ends. The site
// publishes tiers progressively, so the first scrape after 18:35 is often
// partial.
func (s *Service) AwaitComplete(ctx context.Context, day time.Time, interval time.Duration) (domain.Draw, error) {
	if interval <= 0 {
		interval = 20 * time.Second
	}
	attempt := 0
	for {
		attempt++
		draw, err := s.Get(ctx, day)
		switch {
		case err == nil:
			return draw, nil
		case errors.Is(err, ErrNoResult), errors.Is(err, domain.ErrOutOfRange):
			return domain.Draw{}, err // settled; polling cannot help
		}
		s.log.Info("result not ready, will retry",
			"date", domain.FormatISO(day), "attempt", attempt, "reason", err)

		select {
		case <-ctx.Done():
			return domain.Draw{}, fmt.Errorf("%s: gave up after %d attempts: %w",
				domain.FormatVN(day), attempt, err)
		case <-time.After(interval):
		}
	}
}

// Stats exposes archive counts for the status command.
func (s *Service) Stats(ctx context.Context) (storage.Stats, error) { return s.store.Stats(ctx) }
