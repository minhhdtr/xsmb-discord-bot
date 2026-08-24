package bot_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/minhhdtr/xsmb-discord-bot/internal/bot"
	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/provider"
	"github.com/minhhdtr/xsmb-discord-bot/internal/service"
	"github.com/minhhdtr/xsmb-discord-bot/internal/storage"
)

// page renders a result page; tiers limits how many are present, which is how
// the site looks mid-draw.
func page(tiers int) string {
	classes := []string{"special-prize", "prize1", "prize2", "prize3",
		"prize4", "prize5", "prize6", "prize7"}
	var b strings.Builder
	b.WriteString(`<html><body><table class="kqmb">`)
	for i, spec := range domain.PrizeLayout {
		if i >= tiers {
			break
		}
		b.WriteString("<tr>")
		for n := 0; n < spec.Count; n++ {
			b.WriteString(fmt.Sprintf(`<td class="%s"><span>%0*d</span></td>`,
				classes[i], spec.Digits, n+1))
		}
		b.WriteString("</tr>")
	}
	b.WriteString(`</table></body></html>`)
	return b.String()
}

// Real crawler, real service, real Postgres, against a site that publishes
// tiers progressively the way xoso.com.vn does between 18:15 and 18:35.
func TestEndToEndAnnouncement(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()

	store, err := storage.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("OpenPostgres: %v", err)
	}
	defer store.Close()
	reset(t, dsn)

	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// First two requests catch the draw in progress; the third is complete.
		switch atomic.AddInt32(&hits, 1) {
		case 1:
			fmt.Fprint(w, page(3))
		case 2:
			fmt.Fprint(w, page(7))
		default:
			fmt.Fprint(w, page(8))
		}
	}))
	defer server.Close()

	day := domain.NewDate(2026, 8, 21)
	clock := func() time.Time { return time.Date(2026, 8, 21, 18, 35, 0, 0, domain.Location()) }
	src := provider.NewXoso(provider.WithBaseURL(server.URL),
		provider.WithBackoff(time.Millisecond), provider.WithLogger(quiet()))
	svc := service.New(store, src, clock, quiet())

	if _, err := store.Subscribe(ctx, "guild", "channel"); err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	announcer := bot.NewAnnouncer(svc, store, rec.post, quiet())
	announcer.SetPollInterval(time.Millisecond)

	announcer.RunFor(ctx, day)

	if got := rec.channels(); len(got) != 1 || got[0] != "channel" {
		t.Fatalf("posted to %v after %d page loads", got, atomic.LoadInt32(&hits))
	}
	if hits < 3 {
		t.Fatalf("announced after %d loads; a partial page was accepted", hits)
	}

	// The stored result must survive domain validation on the way back out.
	stored, found, err := store.Draw(ctx, day)
	if err != nil || !found {
		t.Fatalf("draw not stored: found=%v err=%v", found, err)
	}
	if len(stored.Prizes.Numbers()) != domain.TotalNumbers || stored.Prizes.Special() != "00001" {
		t.Fatalf("stored draw = %+v", stored.Prizes.Numbers())
	}

	// A second run is a no-op: the claim already exists.
	announcer.RunFor(ctx, day)
	if got := len(rec.channels()); got != 1 {
		t.Fatalf("posted %d times across two runs", got)
	}

	// And a command asking for the same day now answers from the database.
	before := atomic.LoadInt32(&hits)
	router := bot.NewRouter(svc, nil, store, "!xsmb", "!gold", quiet())
	embed := router.Handle(ctx, bot.Request{Args: []string{"21/08/2026"}}).Embed
	if !strings.Contains(embed.Title, "21/08/2026") {
		t.Fatalf("title = %q", embed.Title)
	}
	if atomic.LoadInt32(&hits) != before {
		t.Fatal("a stored day was crawled again")
	}
}

func reset(t *testing.T, dsn string) {
	t.Helper()
	db, err := openDB(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`TRUNCATE draws, absences, subscriptions, announcements`); err != nil {
		t.Fatal(err)
	}
}
