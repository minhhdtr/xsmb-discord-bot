package provider_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/provider"
)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func fixture(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/vangtoday.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return body
}

func fetchedAt() time.Time { return time.Date(2026, 8, 21, 14, 5, 0, 0, domain.Location()) }

func parseFixture(t *testing.T) domain.GoldBoard {
	t.Helper()
	board, err := provider.ParseGold(fixture(t), "vang.today", fetchedAt(), quietLog())
	if err != nil {
		t.Fatalf("ParseGold: %v", err)
	}
	return board
}

func TestParseGoldReadsEveryRow(t *testing.T) {
	board := parseFixture(t)
	if got := len(board.Quotes()); got != 12 {
		t.Fatalf("got %d quotes, want 12", got)
	}
	if got := len(board.Domestic()); got != 11 {
		t.Fatalf("got %d domestic quotes, want 11", got)
	}
}

// The epoch must be read in local time; UTC is seven hours out.
func TestParseGoldResolvesTimestampInLocalTime(t *testing.T) {
	board := parseFixture(t)
	if got := board.UpdatedAt.Format("2006-01-02 15:04"); got != "2026-08-21 14:00" {
		t.Fatalf("UpdatedAt = %s, want the value of the date and time fields", got)
	}
	if name, _ := board.UpdatedAt.Zone(); board.UpdatedAt.Location() != domain.Location() {
		t.Fatalf("UpdatedAt is in zone %s, want Asia/Ho_Chi_Minh", name)
	}
}

func TestParseGoldFallsBackToDateAndTimeFields(t *testing.T) {
	body := []byte(`{"success":true,"timestamp":0,"date":"2026-08-21","time":"14:00","prices":
		{"SJL1L10":{"name":"SJC 9999","buy":1,"sell":2,"currency":"VND"}}}`)
	board, err := provider.ParseGold(body, "test", fetchedAt(), quietLog())
	if err != nil {
		t.Fatal(err)
	}
	if got := board.UpdatedAt.Format("2006-01-02 15:04"); got != "2026-08-21 14:00" {
		t.Fatalf("UpdatedAt = %s", got)
	}
}

// The world row is one-sided. A blanket "sell must exceed buy" rule would
// drop it, or render "Bán 0".
func TestParseGoldKeepsTheOneSidedWorldRow(t *testing.T) {
	board := parseFixture(t)
	world, ok := board.World()
	if !ok {
		t.Fatal("world row was dropped")
	}
	if world.TwoSided() {
		t.Fatal("world row reports a sell price")
	}
	if world.Buy != 4565.4 || world.Currency != domain.USD {
		t.Fatalf("world = %+v", world)
	}
	if world.Name != "Vàng thế giới" {
		t.Fatalf("world name = %q, want the Vietnamese label", world.Name)
	}
}

// prices is a JSON object, so without an explicit order the output would
// shuffle between calls.
func TestParseGoldOrderIsStable(t *testing.T) {
	first := parseFixture(t).Quotes()
	for run := 0; run < 25; run++ {
		next := parseFixture(t).Quotes()
		for i := range first {
			if first[i].Code != next[i].Code {
				t.Fatalf("run %d: position %d was %s, now %s",
					run, i, first[i].Code, next[i].Code)
			}
		}
	}
	if first[0].Code != "XAUUSD" || first[1].Code != "SJL1L10" {
		t.Fatalf("order starts %s, %s", first[0].Code, first[1].Code)
	}
}

// A brand added upstream must appear, not vanish.
func TestParseGoldAppendsUnknownCodes(t *testing.T) {
	body := []byte(`{"success":true,"timestamp":1787295606,"prices":{
		"SJL1L10":{"name":"SJC 9999","buy":143600000,"sell":146600000,"currency":"VND"},
		"ZZZNEW":{"name":"Brand New","buy":1000,"sell":2000,"currency":"VND"},
		"AAANEW":{"name":"Another","buy":1000,"sell":2000,"currency":"VND"}}}`)
	board, err := provider.ParseGold(body, "test", fetchedAt(), quietLog())
	if err != nil {
		t.Fatal(err)
	}
	quotes := board.Quotes()
	if len(quotes) != 3 {
		t.Fatalf("got %d quotes", len(quotes))
	}
	if quotes[0].Code != "SJL1L10" || quotes[1].Code != "AAANEW" || quotes[2].Code != "ZZZNEW" {
		t.Fatalf("order = %s, %s, %s", quotes[0].Code, quotes[1].Code, quotes[2].Code)
	}
	// An unmapped code keeps the upstream label rather than showing a blank.
	if quotes[1].Name != "Another" {
		t.Fatalf("unknown code name = %q", quotes[1].Name)
	}
}

func TestParseGoldSkipsOneBadRowButKeepsTheRest(t *testing.T) {
	body := []byte(`{"success":true,"timestamp":1787295606,"prices":{
		"SJL1L10":{"name":"SJC 9999","buy":143600000,"sell":146600000,"currency":"VND"},
		"BROKEN":{"name":"Swapped","buy":146600000,"sell":143600000,"currency":"VND"}}}`)
	board, err := provider.ParseGold(body, "test", fetchedAt(), quietLog())
	if err != nil {
		t.Fatal(err)
	}
	if got := len(board.Quotes()); got != 1 {
		t.Fatalf("got %d quotes, want the bad row dropped", got)
	}
}

func TestParseGoldRejectsUnusablePayloads(t *testing.T) {
	cases := map[string]string{
		"success false":    `{"success":false,"prices":{}}`,
		"no prices":        `{"success":true,"prices":{}}`,
		"all rows bad":     `{"success":true,"prices":{"X":{"name":"X","buy":0,"sell":0,"currency":"VND"}}}`,
		"unknown currency": `{"success":true,"prices":{"X":{"name":"X","buy":1,"sell":2,"currency":"EUR"}}}`,
		"not json":         `<html>nope</html>`,
	}
	for label, body := range cases {
		if _, err := provider.ParseGold([]byte(body), "test", fetchedAt(), quietLog()); err == nil {
			t.Fatalf("%s: accepted", label)
		}
	}
}

func TestGoldBoardAgeAndSpread(t *testing.T) {
	board := parseFixture(t)
	// The epoch has seconds (14:00:06) while `time` is truncated to the minute.
	if got := board.Age(fetchedAt()); got != 5*time.Minute-6*time.Second {
		t.Fatalf("Age = %v, want 4m54s", got)
	}
	for _, q := range board.Domestic() {
		if q.Spread() <= 0 {
			t.Fatalf("%s has spread %v", q.Code, q.Spread())
		}
	}
	world, _ := board.World()
	if world.Spread() != 0 {
		t.Fatalf("one-sided quote reports spread %v", world.Spread())
	}
}

func TestVangTodayFetchesOverHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(fixture(t))
	}))
	defer server.Close()

	client := provider.NewVangToday(provider.WithGoldURL(server.URL), provider.WithGoldLogger(quietLog()))
	board, err := client.Board(context.Background())
	if err != nil {
		t.Fatalf("Board: %v", err)
	}
	if len(board.Quotes()) != 12 || board.Source != "vang.today" {
		t.Fatalf("board = %d quotes from %q", len(board.Quotes()), board.Source)
	}
}

func TestVangTodaySurfacesHTTPErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := provider.NewVangToday(provider.WithGoldURL(server.URL), provider.WithGoldLogger(quietLog()))
	if _, err := client.Board(context.Background()); err == nil {
		t.Fatal("a 503 was reported as success")
	}
}

func TestVangTodayHonoursContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Write(fixture(t))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	client := provider.NewVangToday(provider.WithGoldURL(server.URL), provider.WithGoldLogger(quietLog()))
	_, err := client.Board(ctx)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want a deadline error", err)
	}
}

func TestZeroGoldBoardIsInert(t *testing.T) {
	var zero domain.GoldBoard
	if zero.Valid() || len(zero.Quotes()) != 0 || len(zero.Domestic()) != 0 {
		t.Fatal("zero board returned data")
	}
	if _, ok := zero.World(); ok {
		t.Fatal("zero board reported a world row")
	}
	if _, err := domain.NewGoldBoard(nil, time.Time{}, "x", time.Time{}); !errors.Is(err, domain.ErrNoQuotes) {
		t.Fatalf("err = %v", err)
	}
	_ = fmt.Sprint(zero.Age(fetchedAt()))
}
