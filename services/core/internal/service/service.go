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

// Defined in domain, and re-exported here so every existing caller and every
// errors.Is check keeps working unchanged.
var (
	ErrNoResult = domain.ErrNoResult
	ErrNotYet   = domain.ErrNotYet
)

// Service reads draws, crawling on demand and caching what it learns.
type Service struct {
	store storage.Store
	src   provider.Provider
	now   func() time.Time
	log   *slog.Logger

	mu       sync.Mutex
	inFlight map[string]*flight

	// missed holds days the source said were not ready yet, with the time the
	// note expires. Guarded separately from mu so a crawl in progress does not
	// hold up a lookup.
	missMu sync.Mutex
	missed map[string]time.Time

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
	return &Service{store: store, src: src, now: clock, log: log,
		inFlight: make(map[string]*flight), missed: make(map[string]time.Time)}
}

// Now exposes the service clock so callers share one notion of time.
func (s *Service) Now() time.Time { return s.now().In(domain.Location()) }

// absenceGrace is how long past 18:35 to wait before believing an empty page
// means the day had no draw at all.
//
// Marking an absence is a one-way door. Get checks IsAbsent before it will
// crawl, a client polling for a day treats ErrNoResult as settled and stops,
// and Backfill goes through Get as well. Only SaveDraw clears an absence, and
// once a day is marked nothing reaches SaveDraw for it again - the day stays
// empty until someone deletes the row by hand. The source publishes tier by
// tier, so a blank or unparseable page just after the mark is far more likely
// to be mid-publication than a cancelled draw. The bar for writing an absence
// is therefore high, and the cost of waiting is only that a genuinely empty
// day is confirmed later.
const absenceGrace = 45 * time.Minute

// negativeTTL is a floor on how often the same day is crawled again after the
// source said it was not ready.
//
// Single-flight collapses requests that overlap; it does nothing for requests
// spaced apart, so a run of commands while the results are still landing
// becomes a run of crawls. This is deliberately below the 20 second poll used
// by Ingest, so it is never throttled by this and needs no way around it.
const negativeTTL = 10 * time.Second

// drawSettles is how long before the completion mark the balls are done and
// the source can plausibly have the numbers.
const drawSettles = 10 * time.Minute

// Latest returns the newest result available. Before 18:35 LatestPublished
// says yesterday, but the draw itself finishes a little earlier, so once it
// has settled this reaches for today first and falls back quietly.
func (s *Service) Latest(ctx context.Context) (domain.Draw, error) {
	now := s.Now()
	today := domain.DayOf(now)
	published := domain.LatestPublished(now)

	if published.Before(today) && !now.Before(domain.CompletionTime(today).Add(-drawSettles)) {
		draw, err := s.Get(ctx, today)
		switch {
		case err == nil:
			return draw, nil
		case errors.Is(err, ErrNotYet), errors.Is(err, ErrNoResult):
			// Ordinary this early. Yesterday's result is the honest answer.
		default:
			return domain.Draw{}, err
		}
	}
	return s.Get(ctx, published)
}

// Get returns one day's result, crawling if it is not stored yet. This is the
// path for anything a person triggered, so it heeds the notes left by earlier
// misses.
func (s *Service) Get(ctx context.Context, day time.Time) (domain.Draw, error) {
	return s.lookup(ctx, day, true)
}

// poll is Get for Ingest, which already paces itself. It ignores the notes,
// because throttling a loop that polls every twenty seconds by a note some
// request left a second ago ties two unrelated rates together. It still
// leaves notes: whether to consult one is the caller's business, whether to
// record one is not.
func (s *Service) poll(ctx context.Context, day time.Time) (domain.Draw, error) {
	return s.lookup(ctx, day, false)
}

func (s *Service) lookup(ctx context.Context, day time.Time, heedNotes bool) (domain.Draw, error) {
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
	// After the store lookup, never before it: a day Ingest has just written
	// must be served, not refused because of a note left while it was missing.
	if heedNotes && s.missedRecently(day) {
		return domain.Draw{}, fmt.Errorf("%s: %w", domain.FormatVN(day), ErrNotYet)
	}

	draw, err := s.fetchOnce(ctx, day)
	if errors.Is(err, ErrNotYet) {
		s.noteMiss(day)
	}
	return draw, err
}

// missedRecently reports whether the source was asked for this day within the
// last negativeTTL and said it was not ready.
func (s *Service) missedRecently(day time.Time) bool {
	s.missMu.Lock()
	defer s.missMu.Unlock()
	until, ok := s.missed[domain.FormatISO(day)]
	return ok && s.Now().Before(until)
}

// noteMiss records that the source had nothing for this day yet. Expired
// entries are swept here rather than on a timer: the map only holds days
// somebody asked about, and every write is a chance to drop the stale ones.
func (s *Service) noteMiss(day time.Time) {
	s.missMu.Lock()
	defer s.missMu.Unlock()

	now := s.Now()
	key := domain.FormatISO(day)
	if s.missed == nil {
		s.missed = make(map[string]time.Time)
	}
	for other, until := range s.missed {
		if other != key && !now.Before(until) {
			delete(s.missed, other)
		}
	}
	s.missed[key] = now.Add(negativeTTL)
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
	// Before the mark plus absenceGrace, a missing result is early news rather
	// than an absence. See absenceGrace for why this errs so far toward
	// waiting.
	due := !s.Now().Before(domain.CompletionTime(day).Add(absenceGrace))

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

// Stats exposes archive counts for the status command.
func (s *Service) Stats(ctx context.Context) (storage.Stats, error) { return s.store.Stats(ctx) }
