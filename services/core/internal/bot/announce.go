package bot

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// Poster sends one embed to one channel.
type Poster func(ctx context.Context, channelID string, embed *discordgo.MessageEmbed) error

// Announcer posts each day's result once 18:35 has passed and the page has
// all 27 numbers.
type Announcer struct {
	core     Core
	now      func() time.Time
	post     Poster
	log      *slog.Logger
	window   time.Duration // how long to keep polling after the draw time
	interval time.Duration // pause between polls
	gap      time.Duration // pause between sends within one worker
	workers  int           // channels posted to at once
}

// NewAnnouncer builds the scheduler. window bounds how long a stalled draw is
// chased before the run is abandoned until tomorrow.
func NewAnnouncer(core Core, clock func() time.Time, post Poster, log *slog.Logger) *Announcer {
	if log == nil {
		log = slog.Default()
	}
	if clock == nil {
		clock = func() time.Time { return time.Now().In(domain.Location()) }
	}
	return &Announcer{
		core: core, now: clock, post: post, log: log,
		window:   45 * time.Minute,
		interval: 20 * time.Second,
		gap:      150 * time.Millisecond,
		workers:  6,
	}
}

// SetPollInterval overrides the retry interval. For tests.
func (a *Announcer) SetPollInterval(d time.Duration) { a.interval = d }

// SetFanOut overrides the worker count and the pause between sends.
func (a *Announcer) SetFanOut(workers int, gap time.Duration) {
	if workers > 0 {
		a.workers = workers
	}
	a.gap = gap
}

// Run blocks until ctx ends, announcing once a day. On startup it catches up
// on today if 18:35 has already passed; the DB claim stops duplicates.
func (a *Announcer) Run(ctx context.Context) {
	if now := a.now(); !now.Before(domain.CompletionTime(now)) {
		a.RunFor(ctx, domain.DayOf(now))
	}
	for {
		wait := a.untilNext()
		a.log.Info("announcer waiting", "next_run", a.now().Add(wait).Format(time.RFC3339))
		select {
		case <-ctx.Done():
			a.log.Info("announcer stopped")
			return
		case <-time.After(wait):
		}
		a.RunFor(ctx, domain.DayOf(a.now()))
	}
}

// untilNext is the delay to the next 18:35. Recomputed each cycle so clock
// drift can't accumulate.
func (a *Announcer) untilNext() time.Duration {
	now := a.now()
	next := domain.CompletionTime(now)
	if !now.Before(next) {
		next = domain.CompletionTime(now.AddDate(0, 0, 1))
	}
	return next.Sub(now)
}

// RunFor waits for day's result, then posts it to every subscribed channel.
func (a *Announcer) RunFor(ctx context.Context, day time.Time) {
	subs, err := a.core.Subscriptions(ctx)
	if err != nil {
		a.log.Error("cannot list subscriptions", "error", err)
		return
	}
	if len(subs) == 0 {
		a.log.Info("no subscribed channels, skipping announcement", "date", domain.FormatISO(day))
		return
	}

	waitCtx, cancel := context.WithTimeout(ctx, a.window)
	defer cancel()

	draw, err := awaitComplete(waitCtx, a.core, day, a.interval)
	if err != nil {
		if errors.Is(err, domain.ErrNoResult) {
			a.log.Info("no draw to announce", "date", domain.FormatISO(day))
			return
		}
		a.log.Error("giving up on announcement", "date", domain.FormatISO(day), "error", err)
		return
	}

	embed := DrawEmbed(draw, true)
	sent := a.fanOut(ctx, day, subs, embed)

	a.log.Info("announcement done", "date", domain.FormatISO(day), "channels", sent)
}

// fanOut posts to all subscribed channels a few at a time. Discord rate
// limits per channel, so different channels can be written to in parallel.
func (a *Announcer) fanOut(ctx context.Context, day time.Time,
	subs []domain.Subscription, embed *discordgo.MessageEmbed) int {

	workers := a.workers
	if workers < 1 {
		workers = 1
	}
	if workers > len(subs) {
		workers = len(subs)
	}

	queue := make(chan domain.Subscription)
	go func() {
		defer close(queue)
		for _, sub := range subs {
			select {
			case queue <- sub:
			case <-ctx.Done():
				return
			}
		}
	}()

	var (
		mu   sync.Mutex
		sent int
		wg   sync.WaitGroup
	)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for sub := range queue {
				if ctx.Err() != nil {
					return
				}
				if a.postTo(ctx, day, sub.ChannelID, embed) {
					mu.Lock()
					sent++
					mu.Unlock()
				}
				if a.gap > 0 {
					select {
					case <-time.After(a.gap):
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}
	wg.Wait()
	return sent
}

// postTo claims one channel and sends, reporting whether a message went out.
func (a *Announcer) postTo(ctx context.Context, day time.Time,
	channelID string, embed *discordgo.MessageEmbed) bool {

	won, err := a.core.ClaimAnnouncement(ctx, day, channelID)
	if err != nil {
		a.log.Error("cannot claim announcement", "channel", channelID, "error", err)
		return false
	}
	if !won {
		return false // already posted, by an earlier run or another instance
	}
	if err := a.post(ctx, channelID, embed); err != nil {
		a.log.Error("cannot post announcement", "channel", channelID, "error", err)
		// Give the claim back so the next run retries.
		if err := a.core.ReleaseAnnouncement(ctx, day, channelID); err != nil {
			a.log.Error("cannot release claim", "channel", channelID, "error", err)
		}
		return false
	}
	return true
}
