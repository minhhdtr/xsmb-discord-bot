package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// Stats are cached until a new draw lands. That's the only thing that can
// change them, so the newest stored day works as an exact cache key.
type statsCache struct {
	mu       sync.Mutex
	builtFor time.Time
	entries  map[string]*statsEntry
}

// statsEntry is one cached answer, or one being built. The lock is never
// held while building, or different statistics would queue behind each
// other.
type statsEntry struct {
	done  chan struct{}
	value any
	err   error
}

// cachedStat memoises one statistic. Same question shares one computation;
// different questions don't wait for each other.
func cachedStat[T any](ctx context.Context, s *Service, key string, build func() (T, error)) (T, error) {
	var zero T

	newest, _, err := s.store.LatestDraw(ctx)
	if err != nil {
		return zero, err
	}

	s.stats.mu.Lock()
	if !s.stats.builtFor.Equal(newest) || s.stats.entries == nil {
		s.stats.builtFor = newest
		s.stats.entries = make(map[string]*statsEntry)
	}
	if existing, running := s.stats.entries[key]; running {
		s.stats.mu.Unlock()
		select {
		case <-existing.done:
		case <-ctx.Done():
			return zero, ctx.Err()
		}
		if existing.err != nil {
			return zero, existing.err
		}
		if value, ok := existing.value.(T); ok {
			return value, nil
		}
		return zero, fmt.Errorf("stats cache holds the wrong type for %q", key)
	}
	entry := &statsEntry{done: make(chan struct{})}
	s.stats.entries[key] = entry
	s.stats.mu.Unlock()

	value, err := build()
	entry.value, entry.err = value, err
	close(entry.done)

	if err != nil {
		// Don't remember failures, or one blip poisons the key until the next draw.
		s.stats.mu.Lock()
		if s.stats.entries[key] == entry {
			delete(s.stats.entries, key)
		}
		s.stats.mu.Unlock()
		return zero, err
	}
	return value, nil
}

// LoGan ranks two-digit tails by how long they have gone unseen.
func (s *Service) LoGan(ctx context.Context, limit int) ([]domain.Gan, error) {
	return cachedStat(ctx, s, fmt.Sprintf("logan:%d", limit), func() ([]domain.Gan, error) {
		return s.store.LoGan(ctx, limit)
	})
}

// DeGan does the same for the special prize alone.
func (s *Service) DeGan(ctx context.Context, limit int) ([]domain.Gan, error) {
	return cachedStat(ctx, s, fmt.Sprintf("degan:%d", limit), func() ([]domain.Gan, error) {
		return s.store.DeGan(ctx, limit)
	})
}

// Frequency counts appearances over the last days draws; zero means all.
func (s *Service) Frequency(ctx context.Context, days int) ([]domain.Frequency, error) {
	return cachedStat(ctx, s, fmt.Sprintf("freq:%d", days), func() ([]domain.Frequency, error) {
		return s.store.Frequency(ctx, days)
	})
}

// Profile gathers one number's history.
func (s *Service) Profile(ctx context.Context, lo string) (domain.Profile, error) {
	return cachedStat(ctx, s, "profile:"+lo, func() (domain.Profile, error) {
		return s.store.Profile(ctx, lo)
	})
}

// SpecialMonth lists one month of special prizes.
func (s *Service) SpecialMonth(ctx context.Context, year int, month time.Month) ([]domain.SpecialDay, error) {
	key := fmt.Sprintf("db:%04d-%02d", year, int(month))
	return cachedStat(ctx, s, key, func() ([]domain.SpecialDay, error) {
		return s.store.SpecialMonth(ctx, year, month)
	})
}
