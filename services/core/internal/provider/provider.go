// Package provider fetches draws from an upstream website.
package provider

import (
	"context"
	"errors"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// Provider is one source of results. A network problem must never be
// reported as "this day has no result".
type Provider interface {
	Name() string
	Fetch(ctx context.Context, day time.Time) domain.Outcome
}

var (
	// ErrNoData means the page loaded but held no prize numbers. Whether that's
	// "no draw" or "not started yet" depends on the clock, which lives in service.
	ErrNoData = errors.New("page has no prize numbers")

	// ErrBlocked means bot protection answered instead of the site. Separate
	// because the fix is to swap the TLS transport, not to retry harder.
	ErrBlocked = errors.New("blocked by bot protection")
)
