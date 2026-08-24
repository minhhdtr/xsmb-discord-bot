package storage_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/storage"
)

func prizes(t *testing.T, special string) domain.Prizes {
	t.Helper()
	numbers := make([]string, 0, domain.TotalNumbers)
	for _, spec := range domain.PrizeLayout {
		for n := 0; n < spec.Count; n++ {
			numbers = append(numbers, strings.Repeat("0", spec.Digits-1)+"1")
		}
	}
	numbers[0] = special
	p, err := domain.NewPrizes(numbers)
	if err != nil {
		t.Fatalf("prizes: %v", err)
	}
	return p
}

// stores runs a test against both implementations, so the in-memory double
// can't drift from Postgres.
func stores(t *testing.T, body func(t *testing.T, store storage.Store)) {
	t.Helper()
	t.Run("memory", func(t *testing.T) { body(t, storage.NewMemory()) })

	t.Run("postgres", func(t *testing.T) {
		dsn := os.Getenv("TEST_DATABASE_URL")
		if dsn == "" {
			t.Skip("TEST_DATABASE_URL not set")
		}
		ctx := context.Background()
		store, err := storage.OpenPostgres(ctx, dsn)
		if err != nil {
			t.Fatalf("OpenPostgres: %v", err)
		}
		t.Cleanup(func() { store.Close() })
		truncate(t, dsn)
		body(t, store)
	})
}

func truncate(t *testing.T, dsn string) {
	t.Helper()
	db, err := openRaw(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`TRUNCATE draws, absences, subscriptions, announcements`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func TestSaveAndReadDraw(t *testing.T) {
	stores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		day := domain.NewDate(2026, 8, 14)
		want := domain.Draw{Date: day, Prizes: prizes(t, "94533"), Source: "xoso.com.vn",
			FetchedAt: time.Now().In(domain.Location())}

		if _, found, err := store.Draw(ctx, day); err != nil || found {
			t.Fatalf("empty store: found=%v err=%v", found, err)
		}
		if err := store.SaveDraw(ctx, want); err != nil {
			t.Fatalf("SaveDraw: %v", err)
		}
		got, found, err := store.Draw(ctx, day)
		if err != nil || !found {
			t.Fatalf("Draw: found=%v err=%v", found, err)
		}
		if got.Prizes.Special() != "94533" || got.Source != "xoso.com.vn" {
			t.Fatalf("got %+v", got)
		}
		if !got.Date.Equal(day) {
			t.Fatalf("date = %s, want %s", got.Date, day)
		}
	})
}

func TestSaveDrawIsIdempotent(t *testing.T) {
	stores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		day := domain.NewDate(2026, 8, 14)
		first := domain.Draw{Date: day, Prizes: prizes(t, "11111"), Source: "a"}
		second := domain.Draw{Date: day, Prizes: prizes(t, "22222"), Source: "b"}
		if err := store.SaveDraw(ctx, first); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveDraw(ctx, second); err != nil {
			t.Fatalf("second save errored: %v", err)
		}
		got, _, _ := store.Draw(ctx, day)
		if got.Prizes.Special() != "11111" {
			t.Fatalf("second write overwrote the first: %q", got.Prizes.Special())
		}
	})
}

func TestSaveDrawRejectsIncomplete(t *testing.T) {
	stores(t, func(t *testing.T, store storage.Store) {
		err := store.SaveDraw(context.Background(),
			domain.Draw{Date: domain.NewDate(2026, 8, 14), Source: "x"})
		if err == nil {
			t.Fatal("stored a draw with zero-value Prizes")
		}
	})
}

func TestAbsenceRoundTripAndClearing(t *testing.T) {
	stores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		day := domain.NewDate(2011, 3, 7)
		if absent, _ := store.IsAbsent(ctx, day); absent {
			t.Fatal("empty store reported an absence")
		}
		if err := store.MarkAbsent(ctx, day); err != nil {
			t.Fatal(err)
		}
		if err := store.MarkAbsent(ctx, day); err != nil {
			t.Fatalf("re-marking errored: %v", err)
		}
		if absent, _ := store.IsAbsent(ctx, day); !absent {
			t.Fatal("absence not recorded")
		}
		// A result appearing later must retire the absence.
		if err := store.SaveDraw(ctx, domain.Draw{Date: day, Prizes: prizes(t, "33333"), Source: "x"}); err != nil {
			t.Fatal(err)
		}
		if absent, _ := store.IsAbsent(ctx, day); absent {
			t.Fatal("absence survived a successful save")
		}
	})
}

func TestSubscriptions(t *testing.T) {
	stores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		added, err := store.Subscribe(ctx, "guild1", "chan1")
		if err != nil || !added {
			t.Fatalf("first subscribe: added=%v err=%v", added, err)
		}
		if added, _ := store.Subscribe(ctx, "guild1", "chan1"); added {
			t.Fatal("duplicate subscribe reported as new")
		}
		if _, err := store.Subscribe(ctx, "guild2", "chan2"); err != nil {
			t.Fatal(err)
		}
		subs, err := store.Subscriptions(ctx)
		if err != nil || len(subs) != 2 {
			t.Fatalf("got %d subscriptions, err=%v", len(subs), err)
		}
		removed, err := store.Unsubscribe(ctx, "chan1")
		if err != nil || !removed {
			t.Fatalf("unsubscribe: removed=%v err=%v", removed, err)
		}
		if removed, _ := store.Unsubscribe(ctx, "chan1"); removed {
			t.Fatal("second unsubscribe reported a removal")
		}
	})
}

// The claim stops a restart mid-window from posting twice.
func TestClaimAnnouncementSucceedsExactlyOnce(t *testing.T) {
	stores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		day := domain.NewDate(2026, 8, 14)
		won, err := store.ClaimAnnouncement(ctx, day, "chan1")
		if err != nil || !won {
			t.Fatalf("first claim: won=%v err=%v", won, err)
		}
		if won, _ := store.ClaimAnnouncement(ctx, day, "chan1"); won {
			t.Fatal("second claim on the same day and channel succeeded")
		}
		if won, _ := store.ClaimAnnouncement(ctx, day, "chan2"); !won {
			t.Fatal("a different channel was blocked")
		}
		if won, _ := store.ClaimAnnouncement(ctx, day.AddDate(0, 0, 1), "chan1"); !won {
			t.Fatal("a different day was blocked")
		}
	})
}

func TestStats(t *testing.T) {
	stores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		for _, d := range []time.Time{
			domain.NewDate(2026, 8, 12),
			domain.NewDate(2026, 8, 14),
			domain.NewDate(2026, 8, 13),
		} {
			if err := store.SaveDraw(ctx, domain.Draw{Date: d, Prizes: prizes(t, "12345"), Source: "x"}); err != nil {
				t.Fatal(err)
			}
		}
		store.MarkAbsent(ctx, domain.NewDate(2005, 10, 2))
		store.Subscribe(ctx, "g", "c")

		s, err := store.Stats(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if s.Draws != 3 || s.Absences != 1 || s.Channels != 1 {
			t.Fatalf("stats = %+v", s)
		}
		if domain.FormatVN(s.Earliest) != "12/08/2026" || domain.FormatVN(s.Latest) != "14/08/2026" {
			t.Fatalf("range = %s..%s", domain.FormatVN(s.Earliest), domain.FormatVN(s.Latest))
		}
	})
}
