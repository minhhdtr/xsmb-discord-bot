package storage_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/storage"
)

// The lease is the one piece of behaviour that lives entirely in SQL. The
// in-memory store deliberately has no expiry, and the API tests run against
// it, so nothing here was ever executed against a database until this file.
//
// It matters because it only fires after a crash: a claim taken but never
// marked sent has to become claimable again, or one badly timed restart
// silences a channel for that day for good.
func postgresOnly(t *testing.T) storage.Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	store, err := storage.OpenPostgres(context.Background(), dsn)
	if err != nil {
		t.Fatalf("OpenPostgres: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	truncate(t, dsn)
	return store
}

// backdate ages a claim, standing in for time passing.
func backdate(t *testing.T, day time.Time, channelID string, by time.Duration) {
	t.Helper()
	db, err := openRaw(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(
		`UPDATE announcements SET posted_at = posted_at - $3::interval
		  WHERE draw_date = $1 AND channel_id = $2`,
		domain.FormatISO(day), channelID, by.String()); err != nil {
		t.Fatal(err)
	}
}

func TestClaimIsHeldForTheLeaseThenReleased(t *testing.T) {
	store := postgresOnly(t)
	ctx := context.Background()
	day := domain.NewDate(2026, 9, 4)

	won, err := store.ClaimAnnouncement(ctx, day, "chan-1")
	if err != nil || !won {
		t.Fatalf("first claim: %v %v", won, err)
	}

	// Still inside the lease: another attempt must stay quiet.
	if won, _ := store.ClaimAnnouncement(ctx, day, "chan-1"); won {
		t.Fatal("a second claim won while the lease was still held")
	}

	// The process died here, so sent_at was never written.
	backdate(t, day, "chan-1", 11*time.Minute)

	if won, err := store.ClaimAnnouncement(ctx, day, "chan-1"); err != nil || !won {
		t.Fatalf("an expired claim was not reclaimable: %v %v", won, err)
	}
}

// Once the message is out the claim is permanent, however long anyone waits.
func TestASentAnnouncementIsNeverReclaimed(t *testing.T) {
	store := postgresOnly(t)
	ctx := context.Background()
	day := domain.NewDate(2026, 9, 4)

	if won, _ := store.ClaimAnnouncement(ctx, day, "chan-1"); !won {
		t.Fatal("first claim lost")
	}
	if err := store.MarkAnnounced(ctx, day, "chan-1"); err != nil {
		t.Fatal(err)
	}

	backdate(t, day, "chan-1", 24*time.Hour)

	if won, _ := store.ClaimAnnouncement(ctx, day, "chan-1"); won {
		t.Fatal("a sent announcement was reclaimed and would be posted twice")
	}
}

// Releasing after a failed send must not wait out the lease.
func TestReleasingMakesAClaimAvailableAtOnce(t *testing.T) {
	store := postgresOnly(t)
	ctx := context.Background()
	day := domain.NewDate(2026, 9, 4)

	if won, _ := store.ClaimAnnouncement(ctx, day, "chan-1"); !won {
		t.Fatal("first claim lost")
	}
	if err := store.ReleaseAnnouncement(ctx, day, "chan-1"); err != nil {
		t.Fatal(err)
	}
	if won, err := store.ClaimAnnouncement(ctx, day, "chan-1"); err != nil || !won {
		t.Fatalf("a released claim was not available: %v %v", won, err)
	}
}

// Days and channels are independent: one channel's lease must not silence
// another's, nor yesterday's today's.
func TestLeasesAreScopedToOneDayAndChannel(t *testing.T) {
	store := postgresOnly(t)
	ctx := context.Background()
	day := domain.NewDate(2026, 9, 4)

	if won, _ := store.ClaimAnnouncement(ctx, day, "chan-1"); !won {
		t.Fatal("first claim lost")
	}
	if won, _ := store.ClaimAnnouncement(ctx, day, "chan-2"); !won {
		t.Error("another channel was blocked")
	}
	if won, _ := store.ClaimAnnouncement(ctx, day.AddDate(0, 0, -1), "chan-1"); !won {
		t.Error("another day was blocked")
	}
}
