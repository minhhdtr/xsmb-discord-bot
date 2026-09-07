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

// gold serves a fixed board and a straight-line history, so the client's gold
// rendering can be exercised without reaching a price site. Enabled with a
// second argument, since most tests want the not_configured path instead.
type gold struct{}

func (gold) Board(context.Context) (domain.GoldBoard, error) {
	return domain.NewGoldBoard([]domain.GoldQuote{
		{Code: "SJC", Name: "SJC Hà Nội", Buy: 8_200_000, Sell: 8_400_000,
			ChangeBuy: 50_000, ChangeSell: 50_000, Currency: domain.VND},
		{Code: "PNJ", Name: "PNJ", Buy: 8_100_000, Sell: 8_300_000,
			ChangeBuy: -20_000, ChangeSell: -30_000, Currency: domain.VND},
		{Code: "XAU", Name: "Vàng thế giới", Buy: 2_412.5,
			ChangeBuy: 7.6, Currency: domain.USD},
	}, time.Now(), "fake", time.Now())
}

func (gold) History(_ context.Context, code string, days int) (domain.GoldSeries, error) {
	points := make([]domain.GoldPoint, 0, days)
	day := domain.DayOf(time.Now()).AddDate(0, 0, -days)
	for i := 0; i < days; i++ {
		points = append(points, domain.GoldPoint{
			Day:  day.AddDate(0, 0, i),
			Buy:  8_000_000 + float64(i)*5_000,
			Sell: 8_200_000 + float64(i)*5_000,
		})
	}
	return domain.NewGoldSeries(code, code, domain.VND, points)
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

	// Gold is off unless asked for, so the default run exercises the
	// not_configured path a client has to handle.
	var prices httpapi.Gold
	if len(os.Args) > 2 && os.Args[2] == "gold" {
		prices = gold{}
	}

	fmt.Println("fakecore listening on", addr, "gold:", prices != nil)
	if err := http.ListenAndServe(addr, httpapi.New(svc, store, prices, log)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
