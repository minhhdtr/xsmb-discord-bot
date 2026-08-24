package storage

import (
	"context"
	"sync"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// Memory is an in-process Store for tests. Production always uses Postgres.
type Memory struct {
	mu       sync.RWMutex
	draws    map[string]domain.Draw
	absences map[string]bool
	channels map[string]Subscription
	claimed  map[string]bool
}

// NewMemory builds an empty in-process store.
func NewMemory() *Memory {
	return &Memory{
		draws:    make(map[string]domain.Draw),
		absences: make(map[string]bool),
		channels: make(map[string]Subscription),
		claimed:  make(map[string]bool),
	}
}

// Close is a no-op.
func (m *Memory) Close() error { return nil }

// Draw reads one day.
func (m *Memory) Draw(_ context.Context, day time.Time) (domain.Draw, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	draw, ok := m.draws[domain.FormatISO(day)]
	return draw, ok, nil
}

// SaveDraw stores a day, ignoring one already present.
func (m *Memory) SaveDraw(_ context.Context, draw domain.Draw) error {
	if !draw.Prizes.Valid() {
		return domain.ErrIncomplete
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := domain.FormatISO(draw.Date)
	if _, exists := m.draws[key]; !exists {
		m.draws[key] = draw
	}
	delete(m.absences, key)
	return nil
}

// IsAbsent reports a confirmed empty day.
func (m *Memory) IsAbsent(_ context.Context, day time.Time) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.absences[domain.FormatISO(day)], nil
}

// MarkAbsent records a confirmed empty day.
func (m *Memory) MarkAbsent(_ context.Context, day time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.absences[domain.FormatISO(day)] = true
	return nil
}

// Subscribe registers a channel.
func (m *Memory) Subscribe(_ context.Context, guildID, channelID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.channels[channelID]; exists {
		return false, nil
	}
	m.channels[channelID] = Subscription{ChannelID: channelID, GuildID: guildID, CreatedAt: time.Now()}
	return true, nil
}

// Unsubscribe removes a channel.
func (m *Memory) Unsubscribe(_ context.Context, channelID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.channels[channelID]; !exists {
		return false, nil
	}
	delete(m.channels, channelID)
	return true, nil
}

// Subscriptions lists registered channels.
func (m *Memory) Subscriptions(_ context.Context) ([]Subscription, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Subscription, 0, len(m.channels))
	for _, s := range m.channels {
		out = append(out, s)
	}
	return out, nil
}

// ClaimAnnouncement succeeds once per day and channel.
func (m *Memory) ClaimAnnouncement(_ context.Context, day time.Time, channelID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := domain.FormatISO(day) + "|" + channelID
	if m.claimed[key] {
		return false, nil
	}
	m.claimed[key] = true
	return true, nil
}

// ReleaseAnnouncement undoes a claim after a failed send.
func (m *Memory) ReleaseAnnouncement(_ context.Context, day time.Time, channelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.claimed, domain.FormatISO(day)+"|"+channelID)
	return nil
}

// Stats summarises the store.
func (m *Memory) Stats(_ context.Context) (Stats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s := Stats{Draws: len(m.draws), Absences: len(m.absences), Channels: len(m.channels)}
	for _, draw := range m.draws {
		if s.Earliest.IsZero() || draw.Date.Before(s.Earliest) {
			s.Earliest = draw.Date
		}
		if s.Latest.IsZero() || draw.Date.After(s.Latest) {
			s.Latest = draw.Date
		}
	}
	return s, nil
}
