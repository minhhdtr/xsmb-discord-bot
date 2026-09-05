// Command fakecore serves the core API over an in-memory archive.
//
// It exists so the TypeScript bot can be tested against the real Go handlers
// rather than against a hand-written mock. A mock would agree with whatever
// the test author believed the contract said, which is exactly the thing worth
// checking once two languages are involved.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/httpapi"
	"github.com/minhhdtr/xsmb-discord-bot/internal/service"
	"github.com/minhhdtr/xsmb-discord-bot/internal/storage"
)

type source struct{}

func (source) Name() string { return "fake" }

func (source) Fetch(_ context.Context, day time.Time) domain.Outcome {
	numbers := make([]string, 0, domain.TotalNumbers)
	at := 0
	for _, spec := range domain.PrizeLayout {
		for n := 0; n < spec.Count; n++ {
			at++
			numbers = append(numbers, fmt.Sprintf("%0*d", spec.Digits, at*7%pow10(spec.Digits)))
		}
	}
	prizes, err := domain.NewPrizes(numbers)
	if err != nil {
		return domain.Failed(err)
	}
	return domain.Found(domain.Draw{Date: domain.DayOf(day), Prizes: prizes,
		Source: "fake", FetchedAt: day})
}

func pow10(n int) int {
	out := 1
	for i := 0; i < n; i++ {
		out *= 10
	}
	return out
}

func main() {
	addr := ":8099"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	store := storage.NewMemory()
	clock := func() time.Time {
		return time.Date(2026, 8, 21, 20, 0, 0, 0, domain.Location())
	}
	svc := service.New(store, source{}, clock, log)

	// Seed a few days so the statistics endpoints have something to say.
	for i := 0; i < 5; i++ {
		_, _ = svc.Get(context.Background(), domain.NewDate(2026, 8, 17+i))
	}

	fmt.Println("fakecore listening on", addr)
	if err := http.ListenAndServe(addr, httpapi.New(svc, store, nil, log)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
