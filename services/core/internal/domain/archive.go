package domain

import "time"

// Archive summarises what the archive holds. It sits in domain rather than in
// storage because it is what the archive *is*, not how it is kept: a client
// reading it over HTTP has no database and should not have to import one to
// name the shape.
type Archive struct {
	Draws    int
	Absences int
	Channels int
	Earliest time.Time
	Latest   time.Time
}

// Subscription is a channel that receives the daily announcement.
//
// The channel id is opaque here. Core stores it and hands it back; only the
// client knows it names a Discord channel. A second platform would add a
// field beside it rather than overload this one.
type Subscription struct {
	ChannelID string
	GuildID   string
	CreatedAt time.Time
}
