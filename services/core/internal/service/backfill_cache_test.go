package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// Filling a gap behind the newest day must still drop the cache. This is the
// backfill case: the archive changes without max(draw_date) moving.
func TestStatsCacheDropsWhenAnOlderDayIsFilled(t *testing.T) {
	svc, _ := statsService(t) // clock sits at 2026-08-21 20:00
	ctx := context.Background()

	if _, err := svc.Get(ctx, domain.NewDate(2026, 8, 20)); err != nil {
		t.Fatal(err)
	}
	first, err := svc.SpecialMonth(ctx, 2026, time.August)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 {
		t.Fatalf("got %d days, want 1", len(first))
	}

	// Backfill reaches an older day. max(draw_date) is unchanged.
	if _, err := svc.Get(ctx, domain.NewDate(2026, 8, 19)); err != nil {
		t.Fatal(err)
	}
	second, err := svc.SpecialMonth(ctx, 2026, time.August)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 2 {
		t.Fatalf("stale cache: got %d days, want 2", len(second))
	}
}
