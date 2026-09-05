package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/provider"
)

// Defaults for a backfill run.
const (
	DefaultConcurrency = 8
	DefaultStopAfter   = 40
)

// BackfillOptions bounds a backfill run.
type BackfillOptions struct {
	// From and To are inclusive. From is clamped to domain.FirstDraw, To to
	// the latest day whose result should exist.
	From time.Time
	To   time.Time
	// Concurrency is how many days are fetched at once. Zero means
	// DefaultConcurrency.
	Concurrency int
	// Rate, if set, is the minimum gap between requests across all workers.
	// Zero means no pacing at all.
	Rate time.Duration
	// StopAfterFailures aborts after this many failures in a row, so a broken
	// source doesn't grind through thousands of identical errors.
	StopAfterFailures int
}

// Report is the outcome of a backfill run.
type Report struct {
	Scanned int // days considered
	Skipped int // already known, no request made
	Fetched int // newly stored
	Absent  int // confirmed to have no draw
	Failed  int
	Aborted bool
	Err     error
}

func (r Report) String() string {
	return fmt.Sprintf("quét %d ngày · lấy mới %d · đã có %d · không có kết quả %d · lỗi %d",
		r.Scanned, r.Fetched, r.Skipped, r.Absent, r.Failed)
}

// Progress is called after each day. With concurrency above one it comes from
// several goroutines, out of order.
type Progress func(day time.Time, report Report)

// Backfill walks newest to oldest, filling gaps. Days already stored or
// already known to be empty cost nothing, so re-running is cheap and
// resuming is automatic.
func (s *Service) Backfill(ctx context.Context, opts BackfillOptions, progress Progress) Report {
	from, to, ok := s.bounds(opts)
	if !ok {
		return Report{}
	}
	workers := opts.Concurrency
	if workers <= 0 {
		workers = DefaultConcurrency
	}
	stopAfter := opts.StopAfterFailures
	if stopAfter <= 0 {
		stopAfter = DefaultStopAfter
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var limiter *time.Ticker
	if opts.Rate > 0 {
		limiter = time.NewTicker(opts.Rate)
		defer limiter.Stop()
	}

	days := make(chan time.Time)
	go func() {
		defer close(days)
		for day := to; !day.Before(from); day = day.AddDate(0, 0, -1) {
			select {
			case days <- day:
			case <-runCtx.Done():
				return
			}
		}
	}()

	run := &backfillRun{svc: s, limiter: limiter, stopAfter: stopAfter, cancel: cancel, progress: progress}

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for day := range days {
				if runCtx.Err() != nil {
					return
				}
				run.one(runCtx, day)
			}
		}()
	}
	wg.Wait()

	run.mu.Lock()
	defer run.mu.Unlock()
	if ctx.Err() != nil && !run.report.Aborted {
		run.report.Aborted, run.report.Err = true, ctx.Err()
	}
	return run.report
}

// bounds resolves the clamped, inclusive day range, or reports that there is
// nothing to do.
func (s *Service) bounds(opts BackfillOptions) (from, to time.Time, ok bool) {
	from = domain.DayOf(opts.From)
	if from.Before(domain.FirstDraw) {
		from = domain.FirstDraw
	}
	to = domain.LatestPublished(s.Now())
	if !opts.To.IsZero() && domain.DayOf(opts.To).Before(to) {
		to = domain.DayOf(opts.To)
	}
	return from, to, !to.Before(from)
}

// backfillRun holds the shared state of one run.
type backfillRun struct {
	svc       *Service
	limiter   *time.Ticker
	stopAfter int
	cancel    context.CancelFunc
	progress  Progress

	mu          sync.Mutex
	report      Report
	consecutive int
}

func (r *backfillRun) one(ctx context.Context, day time.Time) {
	r.mu.Lock()
	r.report.Scanned++
	r.mu.Unlock()

	known, err := r.svc.known(ctx, day)
	if err != nil {
		r.record(day, func(rep *Report) { rep.Failed++ })
		return
	}
	if known {
		r.record(day, func(rep *Report) { rep.Skipped++ })
		return
	}

	if r.limiter != nil {
		select {
		case <-r.limiter.C:
		case <-ctx.Done():
			return
		}
	}

	_, err = r.svc.Get(ctx, day)
	switch {
	case err == nil:
		r.record(day, func(rep *Report) { rep.Fetched++ })
		r.succeeded()
	case errors.Is(err, ErrNoResult):
		r.record(day, func(rep *Report) { rep.Absent++ })
		r.succeeded()
	case errors.Is(err, ErrNotYet):
		// Today, before the draw finished. Nothing to do and not an error.
		r.record(day, nil)
		r.succeeded()
	case ctx.Err() != nil:
		return // the run is already stopping; not a real failure
	default:
		r.svc.log.Warn("backfill could not fetch a day",
			"date", domain.FormatISO(day), "error", err)
		r.failed(day, err)
	}
}

func (r *backfillRun) record(day time.Time, apply func(*Report)) {
	r.mu.Lock()
	if apply != nil {
		apply(&r.report)
	}
	snapshot := r.report
	r.mu.Unlock()
	if r.progress != nil {
		r.progress(day, snapshot)
	}
}

func (r *backfillRun) succeeded() {
	r.mu.Lock()
	r.consecutive = 0
	r.mu.Unlock()
}

func (r *backfillRun) failed(day time.Time, err error) {
	r.mu.Lock()
	r.report.Failed++
	r.consecutive++
	stop := errors.Is(err, provider.ErrBlocked) || r.consecutive >= r.stopAfter
	if stop && !r.report.Aborted {
		r.report.Aborted = true
		r.report.Err = fmt.Errorf("dừng sau %d lỗi liên tiếp: %w", r.consecutive, err)
	}
	snapshot := r.report
	r.mu.Unlock()

	if r.progress != nil {
		r.progress(day, snapshot)
	}
	if stop {
		r.cancel()
	}
}

// known reports whether a day needs no request at all.
func (s *Service) known(ctx context.Context, day time.Time) (bool, error) {
	if _, found, err := s.store.Draw(ctx, day); err != nil {
		return false, err
	} else if found {
		return true, nil
	}
	return s.store.IsAbsent(ctx, day)
}
