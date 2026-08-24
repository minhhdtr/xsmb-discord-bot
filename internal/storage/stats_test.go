package storage_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/storage"
)

// fillerTail pads a draw out to 27 prizes. Tests must not ask about it.
const fillerTail = "99"

// drawWith builds a draw with the given tails, padded with fillerTail so the
// prize widths stay legal.
func drawWith(t *testing.T, day time.Time, tails ...string) domain.Draw {
	t.Helper()
	if len(tails) > domain.TotalNumbers {
		t.Fatalf("%d tails, max %d", len(tails), domain.TotalNumbers)
	}
	numbers := make([]string, 0, domain.TotalNumbers)
	at := 0
	for _, spec := range domain.PrizeLayout {
		for n := 0; n < spec.Count; n++ {
			tail := fillerTail
			if at < len(tails) {
				tail = tails[at]
			}
			numbers = append(numbers, fmt.Sprintf("%0*s", spec.Digits-2, "")+tail)
			at++
		}
	}
	for i, n := range numbers {
		if len(n) < 2 {
			t.Fatalf("number %d too short: %q", i, n)
		}
	}
	prizes, err := domain.NewPrizes(pad(numbers))
	if err != nil {
		t.Fatalf("build draw: %v", err)
	}
	return domain.Draw{Date: domain.DayOf(day), Prizes: prizes, Source: "test"}
}

// pad left-fills each number with zeros to the width its tier requires.
func pad(numbers []string) []string {
	out := make([]string, 0, len(numbers))
	at := 0
	for _, spec := range domain.PrizeLayout {
		for n := 0; n < spec.Count; n++ {
			value := numbers[at]
			for len(value) < spec.Digits {
				value = "0" + value
			}
			out = append(out, value)
			at++
		}
	}
	return out
}

// seed fills a store with one draw per day, oldest first.
func seed(t *testing.T, store storage.Store, start time.Time, perDay [][]string) {
	t.Helper()
	ctx := context.Background()
	for i, tails := range perDay {
		if err := store.SaveDraw(ctx, drawWith(t, start.AddDate(0, 0, i), tails...)); err != nil {
			t.Fatalf("day %d: %v", i, err)
		}
	}
}

func TestLatestDraw(t *testing.T) {
	stores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		if _, ok, err := store.LatestDraw(ctx); err != nil || ok {
			t.Fatalf("empty store: ok=%v err=%v", ok, err)
		}
		seed(t, store, domain.NewDate(2026, 8, 1), [][]string{{"01"}, {"02"}, {"03"}})
		day, ok, err := store.LatestDraw(ctx)
		if err != nil || !ok || domain.FormatVN(day) != "03/08/2026" {
			t.Fatalf("latest = %s ok=%v err=%v", domain.FormatVN(day), ok, err)
		}
	})
}

// A gap of n days between appearances is n-1 days absent: the 1st then the
// 5th is three days, not four.
func TestGanCountsDaysAbsentNotTheInterval(t *testing.T) {
	stores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		// "07" appears on 01/08 and 05/08; the archive ends on 08/08.
		seed(t, store, domain.NewDate(2026, 8, 1), [][]string{
			{"07"}, {"11"}, {"12"}, {"13"}, {"07"}, {"14"}, {"15"}, {"16"},
		})
		gan, err := store.LoGan(ctx, 100)
		if err != nil {
			t.Fatal(err)
		}
		var seven domain.Gan
		for _, g := range gan {
			if g.Number == "07" {
				seven = g
			}
		}
		if seven.Number == "" {
			t.Fatal("07 missing from the ranking")
		}
		if domain.FormatVN(seven.LastSeen) != "05/08/2026" {
			t.Fatalf("last seen = %s", domain.FormatVN(seven.LastSeen))
		}
		if seven.Days != 3 {
			t.Fatalf("current gan = %d, want 3", seven.Days)
		}
		if seven.Record != 3 {
			t.Fatalf("record = %d, want 3 (absent on 02, 03, 04)", seven.Record)
		}
		if domain.FormatVN(seven.RecordEnd) != "05/08/2026" {
			t.Fatalf("record ended %s", domain.FormatVN(seven.RecordEnd))
		}
	})
}

func TestLoGanRanksLongestFirstAndLimits(t *testing.T) {
	stores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		seed(t, store, domain.NewDate(2026, 8, 1), [][]string{
			{"01", "02", "03"}, {"02", "03"}, {"03"}, {"04"},
		})
		gan, err := store.LoGan(ctx, 3)
		if err != nil {
			t.Fatal(err)
		}
		if len(gan) != 3 {
			t.Fatalf("got %d entries, want the limit of 3", len(gan))
		}
		for i := 1; i < len(gan); i++ {
			if gan[i].Days > gan[i-1].Days {
				t.Fatalf("not sorted: %+v", gan)
			}
		}
		if gan[0].Number != "01" || gan[0].Days != 3 {
			t.Fatalf("longest drought = %+v, want 01 at 3 days", gan[0])
		}
	})
}

// Đề is one number a day, a much thinner stream than the 27 lô.
func TestDeGanLooksOnlyAtTheSpecialPrize(t *testing.T) {
	stores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		// "05" is the special prize on day one and a lesser prize on day three, which
		// đề must ignore.
		seed(t, store, domain.NewDate(2026, 8, 1), [][]string{
			{"05", "21"}, {"22", "23"}, {"24", "05"}, {"25", "26"},
		})
		de, err := store.DeGan(ctx, 100)
		if err != nil {
			t.Fatal(err)
		}
		var five domain.Gan
		for _, g := range de {
			if g.Number == "05" {
				five = g
			}
		}
		if five.Number == "" {
			t.Fatal("05 missing from đề gan")
		}
		if five.Days != 3 {
			t.Fatalf("đề gan for 05 = %d, want 3; a lesser prize leaked in", five.Days)
		}
	})
}

// Two prizes in one draw can share a tail: hits counts twice, days once.
func TestFrequencySeparatesHitsFromDays(t *testing.T) {
	stores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		seed(t, store, domain.NewDate(2026, 8, 1), [][]string{
			{"42", "42", "42"}, {"42"}, {"17"},
		})
		freq, err := store.Frequency(ctx, 0)
		if err != nil {
			t.Fatal(err)
		}
		var forty2 domain.Frequency
		for _, f := range freq {
			if f.Number == "42" {
				forty2 = f
			}
		}
		if forty2.Hits != 4 {
			t.Fatalf("hits = %d, want 4", forty2.Hits)
		}
		if forty2.Days != 2 {
			t.Fatalf("days = %d, want 2", forty2.Days)
		}
	})
}

func TestFrequencyHonoursTheWindow(t *testing.T) {
	stores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		// "33" only on the oldest day, "44" only on the newest.
		seed(t, store, domain.NewDate(2026, 8, 1), [][]string{
			{"33"}, {"55"}, {"55"}, {"55"}, {"44"},
		})
		recent, err := store.Frequency(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}
		inWindow := map[string]int{}
		for _, f := range recent {
			inWindow[f.Number] = f.Hits
		}
		if inWindow["44"] == 0 {
			t.Fatal("the newest day fell outside a two-day window")
		}
		if inWindow["33"] != 0 {
			t.Fatalf("a five-day-old draw leaked into a two-day window: %v", inWindow)
		}
	})
}

func TestProfileGathersWindowsAndCycle(t *testing.T) {
	stores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		// "88" on 01, 03 and 07 August; archive ends 09 August.
		seed(t, store, domain.NewDate(2026, 8, 1), [][]string{
			{"88", "88"}, {"11"}, {"88"}, {"12"}, {"13"}, {"14"}, {"88"}, {"15"}, {"16"},
		})
		profile, err := store.Profile(ctx, "88")
		if err != nil {
			t.Fatal(err)
		}
		if profile.Archive != 9 {
			t.Fatalf("archive = %d draws", profile.Archive)
		}
		if domain.FormatVN(profile.First) != "01/08/2026" {
			t.Fatalf("first = %s", domain.FormatVN(profile.First))
		}
		if profile.Gan.Days != 2 {
			t.Fatalf("gan = %d, want 2", profile.Gan.Days)
		}
		// Absent on 04, 05, 06 - three days, the longest run.
		if profile.Gan.Record != 3 {
			t.Fatalf("record = %d, want 3", profile.Gan.Record)
		}
		// Seen on three days spanning six days: mean cycle 3.
		if profile.AvgCycle != 3 {
			t.Fatalf("cycle = %v, want 3", profile.AvgCycle)
		}
		whole := profile.Windows[len(profile.Windows)-1]
		if whole.Hits != 4 || whole.Draws != 3 || whole.Total != 9 {
			t.Fatalf("whole archive window = %+v", whole)
		}
	})
}

func TestProfileOfANumberNeverSeen(t *testing.T) {
	stores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		seed(t, store, domain.NewDate(2026, 8, 1), [][]string{{"01"}, {"02"}})
		profile, err := store.Profile(ctx, "77")
		if err != nil {
			t.Fatal(err)
		}
		if !profile.Gan.LastSeen.IsZero() || profile.Gan.Days != 0 {
			t.Fatalf("profile = %+v", profile.Gan)
		}
		if profile.Archive != 2 {
			t.Fatalf("archive = %d", profile.Archive)
		}
	})
}

func TestProfileOnAnEmptyArchive(t *testing.T) {
	stores(t, func(t *testing.T, store storage.Store) {
		profile, err := store.Profile(context.Background(), "88")
		if err != nil {
			t.Fatal(err)
		}
		if profile.Archive != 0 || len(profile.Windows) != 0 {
			t.Fatalf("profile = %+v", profile)
		}
	})
}

func TestSpecialMonthCoversExactlyTheMonth(t *testing.T) {
	stores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		// 30 July through 2 August.
		seed(t, store, domain.NewDate(2026, 7, 30), [][]string{
			{"70"}, {"71"}, {"80"}, {"81"},
		})
		days, err := store.SpecialMonth(ctx, 2026, time.August)
		if err != nil {
			t.Fatal(err)
		}
		if len(days) != 2 {
			t.Fatalf("got %d days, want the two in August", len(days))
		}
		if domain.FormatVN(days[0].Day) != "01/08/2026" || days[0].De != "80" {
			t.Fatalf("first = %+v", days[0])
		}
		if len(days[0].Special) != 5 {
			t.Fatalf("special prize %q is not five digits", days[0].Special)
		}
	})
}

func TestSpecialMonthOfAnEmptyMonth(t *testing.T) {
	stores(t, func(t *testing.T, store storage.Store) {
		seed(t, store, domain.NewDate(2026, 8, 1), [][]string{{"01"}})
		days, err := store.SpecialMonth(context.Background(), 2011, time.March)
		if err != nil || len(days) != 0 {
			t.Fatalf("got %d days, err = %v", len(days), err)
		}
	})
}

func TestProfileRecentStrip(t *testing.T) {
	stores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		// "88" twice on 01/08, once on 03/08, nothing after.
		seed(t, store, domain.NewDate(2026, 8, 1), [][]string{
			{"88", "88"}, {"11"}, {"88"}, {"12"}, {"13"},
		})
		profile, err := store.Profile(ctx, "88")
		if err != nil {
			t.Fatal(err)
		}
		if len(profile.Recent) != 5 {
			t.Fatalf("got %d days, want one per draw", len(profile.Recent))
		}
		want := []int{2, 0, 1, 0, 0}
		for i, d := range profile.Recent {
			if d.Hits != want[i] {
				t.Fatalf("day %d (%s) = %d hits, want %d",
					i, domain.FormatVN(d.Day), d.Hits, want[i])
			}
		}
		// Oldest first, so the strip reads left to right in time order.
		if domain.FormatVN(profile.Recent[0].Day) != "01/08/2026" {
			t.Fatalf("strip starts at %s", domain.FormatVN(profile.Recent[0].Day))
		}
	})
}

// The strip and the 30-day window are computed by different routes, so their
// totals agreeing is a real check rather than a restatement.
func TestRecentStripAgreesWithTheThirtyDayWindow(t *testing.T) {
	stores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		perDay := make([][]string, 40)
		for i := range perDay {
			perDay[i] = []string{"88", fmt.Sprintf("%02d", i%100)}
			if i%3 == 0 {
				perDay[i] = append(perDay[i], "88")
			}
		}
		seed(t, store, domain.NewDate(2026, 7, 1), perDay)

		profile, err := store.Profile(ctx, "88")
		if err != nil {
			t.Fatal(err)
		}
		if len(profile.Recent) != domain.RecentDays {
			t.Fatalf("strip holds %d days, want %d", len(profile.Recent), domain.RecentDays)
		}
		total, seen := 0, 0
		for _, d := range profile.Recent {
			total += d.Hits
			if d.Hits > 0 {
				seen++
			}
		}
		var window domain.Window
		for _, w := range profile.Windows {
			if w.Days == 30 {
				window = w
			}
		}
		if total != window.Hits {
			t.Fatalf("strip counts %d hits, the 30-day window counts %d", total, window.Hits)
		}
		if seen != window.Draws {
			t.Fatalf("strip has %d days with hits, the window says %d", seen, window.Draws)
		}
	})
}

func TestRecentStripOnAShortArchive(t *testing.T) {
	stores(t, func(t *testing.T, store storage.Store) {
		seed(t, store, domain.NewDate(2026, 8, 1), [][]string{{"01"}, {"02"}})
		profile, err := store.Profile(context.Background(), "55")
		if err != nil {
			t.Fatal(err)
		}
		if len(profile.Recent) != 2 {
			t.Fatalf("got %d days", len(profile.Recent))
		}
		for _, d := range profile.Recent {
			if d.Hits != 0 {
				t.Fatalf("%s = %d hits for a number never drawn", domain.FormatVN(d.Day), d.Hits)
			}
		}
	})
}
