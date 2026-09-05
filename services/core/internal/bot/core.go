package bot

import (
	"context"
	"errors"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/coreclient"
	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// Core is everything the bot needs from core, declared here on the consuming
// side rather than exported from the provider.
//
// Written this way the bot states its own requirements: the list is exactly as
// long as the commands need, and anything core grows later does not become the
// bot's business by default. It also means a test can supply a stand-in
// without a database, and that the switch from an in-process service to an
// HTTP client changed no code below this line.
type Core interface {
	Latest(ctx context.Context) (domain.Draw, error)
	Draw(ctx context.Context, day time.Time) (domain.Draw, error)

	LoGan(ctx context.Context, limit int) ([]domain.Gan, error)
	DeGan(ctx context.Context, limit int) ([]domain.Gan, error)
	Frequency(ctx context.Context, days int) ([]domain.Frequency, error)
	Profile(ctx context.Context, lo string) (domain.Profile, error)
	SpecialMonth(ctx context.Context, year int, month time.Month) ([]domain.SpecialDay, error)
	Archive(ctx context.Context) (domain.Archive, error)

	Spin(ctx context.Context) (domain.Spin, error)

	GoldBoard(ctx context.Context) (domain.GoldBoard, error)
	GoldHistory(ctx context.Context, code string, days int) (domain.GoldSeries, error)

	Subscriptions(ctx context.Context) ([]domain.Subscription, error)
	Subscribe(ctx context.Context, guildID, channelID string) (bool, error)
	Unsubscribe(ctx context.Context, channelID string) (bool, error)
	ClaimAnnouncement(ctx context.Context, day time.Time, channelID string) (bool, error)
	ReleaseAnnouncement(ctx context.Context, day time.Time, channelID string) error
}

// ErrNotConfigured means core has the feature switched off. Declared here so
// the bot does not import the client package to recognise it; the interface is
// the whole surface between them.
var ErrNotConfigured = coreclient.ErrNotConfigured

// awaitComplete polls core until the day's result is in the archive.
//
// This replaces service.AwaitComplete, which crawled. The bot no longer
// crawls anything: core's Ingest fills the archive on its own schedule, and
// the bot's job is to notice. Asking rather than being pushed keeps the
// dependency pointing one way - the bot knows where core is, core knows
// nothing about the bot - so a second client can be added without touching
// core at all.
func awaitComplete(ctx context.Context, core Core, day time.Time,
	interval time.Duration) (domain.Draw, error) {

	if interval <= 0 {
		interval = 20 * time.Second
	}
	for {
		draw, err := core.Draw(ctx, day)
		switch {
		case err == nil:
			return draw, nil
		case errors.Is(err, domain.ErrNotYet):
			// Still landing. Anything else is settled, or is a real failure,
			// and no amount of waiting changes either.
		default:
			return domain.Draw{}, err
		}

		select {
		case <-ctx.Done():
			return domain.Draw{}, ctx.Err()
		case <-time.After(interval):
		}
	}
}
