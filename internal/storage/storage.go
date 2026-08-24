// Package storage persists draws and Discord subscriptions in PostgreSQL.
package storage

import (
	"context"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// Subscription is a channel that receives the 18:35 announcement.
type Subscription struct {
	ChannelID string
	GuildID   string
	CreatedAt time.Time
}

// Stats summarises what the archive holds.
type Stats struct {
	Draws    int
	Absences int
	Channels int
	Earliest time.Time
	Latest   time.Time
}

// Store is everything the bot needs from persistence.
type Store interface {
	// Draw returns a stored draw; the bool is false when the day isn't stored.
	Draw(ctx context.Context, day time.Time) (domain.Draw, bool, error)
	// SaveDraw stores a draw, ignoring a day already present.
	SaveDraw(ctx context.Context, draw domain.Draw) error
	// IsAbsent reports a day the source confirmed has no result.
	IsAbsent(ctx context.Context, day time.Time) (bool, error)
	// MarkAbsent records such a day so it is not crawled again.
	MarkAbsent(ctx context.Context, day time.Time) error

	// Subscribe registers a channel; the bool reports whether it is new.
	Subscribe(ctx context.Context, guildID, channelID string) (bool, error)
	// Unsubscribe removes a channel; the bool reports whether it existed.
	Unsubscribe(ctx context.Context, channelID string) (bool, error)
	// Subscriptions lists every channel awaiting announcements.
	Subscriptions(ctx context.Context) ([]Subscription, error)

	// ClaimAnnouncement reserves the right to post day's result to channelID.
	// True exactly once per pair, across restarts and instances.
	ClaimAnnouncement(ctx context.Context, day time.Time, channelID string) (bool, error)
	// ReleaseAnnouncement undoes a claim after a failed send.
	ReleaseAnnouncement(ctx context.Context, day time.Time, channelID string) error

	Stats(ctx context.Context) (Stats, error)

	// LatestDraw is the newest day held. Also the cache key for the statistics
	// below - they only change when a new draw lands.
	LatestDraw(ctx context.Context) (time.Time, bool, error)

	// LoGan ranks tails by how long they've gone unseen. DeGan does the same for
	// the special prize alone.
	LoGan(ctx context.Context, limit int) ([]domain.Gan, error)
	DeGan(ctx context.Context, limit int) ([]domain.Gan, error)

	// Frequency counts appearances over the last n days, for every tail.
	Frequency(ctx context.Context, days int) ([]domain.Frequency, error)

	// Profile gathers one number's history.
	Profile(ctx context.Context, lo string) (domain.Profile, error)

	// SpecialMonth lists one month of special prizes.
	SpecialMonth(ctx context.Context, year int, month time.Month) ([]domain.SpecialDay, error)

	Close() error
}
