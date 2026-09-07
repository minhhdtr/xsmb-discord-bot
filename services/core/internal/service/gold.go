package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/provider"
)

// Gold serves the current board behind a short cache. Nothing here touches
// Postgres: a gold price is only ever "now", unlike a draw.
type Gold struct {
	src   provider.GoldProvider
	ttl   time.Duration
	grace time.Duration
	now   func() time.Time
	log   *slog.Logger

	fetchMu sync.Mutex // serialises refreshes, so a burst causes one request

	mu     sync.RWMutex
	board  domain.GoldBoard
	loaded time.Time

	histMu sync.Mutex
	hist   map[string]cachedSeries
}

// cachedSeries is one memoised history request.
type cachedSeries struct {
	series domain.GoldSeries
	loaded time.Time
}

// NewGold builds the gold service. ttl is how long a board is reused; grace is
// how long a stale one may still be served when the source is down.
func NewGold(src provider.GoldProvider, ttl, grace time.Duration, clock func() time.Time, log *slog.Logger) *Gold {
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	if grace <= 0 {
		grace = 30 * time.Minute
	}
	if clock == nil {
		clock = func() time.Time { return time.Now().In(domain.Location()) }
	}
	if log == nil {
		log = slog.Default()
	}
	return &Gold{src: src, ttl: ttl, grace: grace, now: clock, log: log,
		hist: make(map[string]cachedSeries)}
}

// Board returns the current board, refetching once the cache expires. If the
// source fails a recent board is served instead of an error - the embed
// prints its timestamp so the age is visible.
func (g *Gold) Board(ctx context.Context) (domain.GoldBoard, error) {
	if board, ok := g.cached(g.ttl); ok {
		return board, nil
	}

	g.fetchMu.Lock()
	defer g.fetchMu.Unlock()

	// Another caller may have refreshed while this one waited for the lock.
	if board, ok := g.cached(g.ttl); ok {
		return board, nil
	}

	board, err := g.src.Board(ctx)
	if err != nil {
		if stale, ok := g.cached(g.grace); ok {
			g.log.Warn("serving a stale gold board", "error", err,
				"age", g.now().Sub(stale.FetchedAt).Round(time.Second))
			return stale, nil
		}
		return domain.GoldBoard{}, err
	}

	g.mu.Lock()
	g.board, g.loaded = board, g.now()
	g.mu.Unlock()
	return board, nil
}

// History is cached much longer than the live board - a closed day cannot
// change.
//
// Two things this deliberately does not do. It does not hold the lock across
// the upstream call: the source taking ten seconds used to block every other
// chart request behind it, for a fetch none of them needed. And it does not
// serve a stale series forever - a source that has been down for three days is
// broken, and answering with three-day-old prices as though they were current
// hides that from everyone.
func (g *Gold) History(ctx context.Context, code string, days int) (domain.GoldSeries, error) {
	key := fmt.Sprintf("%s|%d", strings.ToUpper(code), days)

	if entry, ok := g.readHistory(key); ok && g.now().Sub(entry.loaded) <= historyTTL {
		return entry.series, nil
	}

	// Outside the lock. Two requests for the same key may both fetch; that
	// costs one extra request and is far cheaper than serialising every
	// caller behind the slowest one.
	series, err := g.src.History(ctx, code, days)
	if err != nil {
		entry, ok := g.readHistory(key)
		if ok && g.now().Sub(entry.loaded) <= historyTTL+historyGrace {
			g.log.Warn("serving a stale gold history", "code", code,
				"age", g.now().Sub(entry.loaded), "error", err)
			return entry.series, nil
		}
		return domain.GoldSeries{}, err
	}

	g.histMu.Lock()
	g.hist[key] = cachedSeries{series: series, loaded: g.now()}
	g.histMu.Unlock()
	return series, nil
}

const (
	// historyTTL is how long a series is served without asking again.
	historyTTL = 30 * time.Minute
	// historyGrace is how much longer it may be served after the source starts
	// failing. Past this the request fails, because a chart that is a day out
	// of date and says nothing about it is worse than an error.
	historyGrace = 6 * time.Hour
)

func (g *Gold) readHistory(key string) (cachedSeries, bool) {
	g.histMu.Lock()
	defer g.histMu.Unlock()
	entry, ok := g.hist[key]
	return entry, ok
}

// cached returns the held board if it is younger than window.
func (g *Gold) cached(window time.Duration) (domain.GoldBoard, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if !g.board.Valid() || g.loaded.IsZero() {
		return domain.GoldBoard{}, false
	}
	if g.now().Sub(g.loaded) > window {
		return domain.GoldBoard{}, false
	}
	return g.board, true
}
