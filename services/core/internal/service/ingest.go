package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// Ingest keeps the archive level with the source, on its own, whether or not
// anybody is watching.
//
// It is a reconcile loop rather than a schedule. Each pass asks two questions
// from scratch - what is the newest day that should be out, and does the
// archive have it - so there is no mark to miss and nothing to remember
// between passes. A tick that runs late, a restart at 18:40, or a source that
// takes twenty minutes to finish publishing all come out right on the next
// pass without any special handling. That is the whole reason there is no job
// queue here: the work to do is derivable from the data, so it never needs to
// be recorded anywhere.
//
// Announcing is a separate concern and lives in the bot. Before this existed
// the crawl was a side effect of announcing, which meant that turning off the
// last subscription silently stopped the archive from updating.
type Ingest struct {
	svc *Service
	log *slog.Logger

	fast time.Duration // pace while results are landing
	slow time.Duration // pace the rest of the day
	lead time.Duration // how long before the mark the landing window opens
	tail time.Duration // how long after it closes
}

// NewIngest builds the loop with paces suited to how the source behaves: the
// balls finish shortly before 18:35, and the page then fills in tier by tier
// over the following several minutes.
func NewIngest(svc *Service, log *slog.Logger) *Ingest {
	if log == nil {
		log = slog.Default()
	}
	return &Ingest{
		svc: svc, log: log,
		fast: 20 * time.Second,
		slow: 15 * time.Minute,
		lead: drawSettles,
		tail: absenceGrace,
	}
}

// SetPace overrides the two intervals. For tests.
func (i *Ingest) SetPace(fast, slow time.Duration) {
	if fast > 0 {
		i.fast = fast
	}
	if slow > 0 {
		i.slow = slow
	}
}

// Run reconciles until ctx ends.
func (i *Ingest) Run(ctx context.Context) {
	i.log.Info("ingest started", "fast", i.fast, "slow", i.slow)
	for {
		i.CatchUp(ctx)
		select {
		case <-ctx.Done():
			i.log.Info("ingest stopped")
			return
		case <-time.After(i.interval(i.svc.Now())):
		}
	}
}

// CatchUp runs one pass. Exported so a test, or a command, can reconcile once
// without starting the loop.
func (i *Ingest) CatchUp(ctx context.Context) {
	now := i.svc.Now()

	// Inside the landing window today's numbers may already be up while
	// LatestPublished still says yesterday, so ask for today by name. Outside
	// it, that would be asking for a draw that has not happened.
	if i.landing(now) && i.reach(ctx, domain.DayOf(now)) {
		return
	}
	i.reach(ctx, domain.LatestPublished(now))
}

// reach fetches a day and reports whether there is anything left to chase.
func (i *Ingest) reach(ctx context.Context, day time.Time) bool {
	switch _, err := i.svc.poll(ctx, day); {
	case err == nil:
		return true
	case errors.Is(err, ErrNotYet):
		return false // ordinary inside the window; try again next pass
	case errors.Is(err, ErrNoResult), errors.Is(err, domain.ErrOutOfRange):
		return true // settled, and polling cannot change it
	default:
		i.log.Warn("ingest could not reach the source",
			"date", domain.FormatISO(day), "error", err)
		return false
	}
}

// interval polls hard while results are landing and idles otherwise. The slow
// pace is not idle busywork: it is what closes the gap after a restart, or
// after a day the source published late.
func (i *Ingest) interval(now time.Time) time.Duration {
	if i.landing(now) {
		return i.fast
	}
	return i.slow
}

// landing is the stretch around the draw when the result appears: a little
// before the mark, because the balls finish first, until well after it,
// because the page fills in gradually.
func (i *Ingest) landing(now time.Time) bool {
	mark := domain.CompletionTime(now)
	return !now.Before(mark.Add(-i.lead)) && now.Before(mark.Add(i.tail))
}
