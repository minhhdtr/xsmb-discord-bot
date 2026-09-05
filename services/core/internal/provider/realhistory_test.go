package provider_test

import (
	"os"
	"testing"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/provider"
)

// Real response from GET /api/prices?type=DOHNL&days=30. Days arrive newest
// first and each one nests its quotes under the type code.
func TestParseGoldHistoryReadsTheLiveShape(t *testing.T) {
	body, err := os.ReadFile("testdata/history_dohnl.json")
	if err != nil {
		t.Fatal(err)
	}
	series, err := provider.ParseGoldHistory(body, "DOHNL", "vang.today")
	if err != nil {
		t.Fatalf("ParseGoldHistory: %v", err)
	}

	if series.Len() != 30 {
		t.Fatalf("got %d points, want 30", series.Len())
	}
	if series.Code != "DOHNL" || series.Currency != domain.VND {
		t.Fatalf("series = %s / %s", series.Code, series.Currency)
	}
	if series.Name != "DOJI Hà Nội" {
		t.Fatalf("name = %q, want the Vietnamese label", series.Name)
	}

	// The payload runs newest first; the series must come back oldest first.
	points := series.Points()
	if domain.FormatVN(points[0].Day) != "24/07/2026" {
		t.Fatalf("first day = %s", domain.FormatVN(points[0].Day))
	}
	if domain.FormatVN(points[29].Day) != "22/08/2026" {
		t.Fatalf("last day = %s", domain.FormatVN(points[29].Day))
	}
	for i := 1; i < len(points); i++ {
		if !points[i].Day.After(points[i-1].Day) {
			t.Fatalf("day %d is not after day %d", i, i-1)
		}
	}

	if points[0].Buy != 135_500_000 || points[0].Sell != 140_500_000 {
		t.Fatalf("oldest point = %+v", points[0])
	}
	if points[29].Buy != 144_000_000 || points[29].Sell != 147_000_000 {
		t.Fatalf("newest point = %+v", points[29])
	}

	low, high := series.Range()
	if low != 135_500_000 || high != 147_000_000 {
		t.Fatalf("range = %v..%v", low, high)
	}
	if got := series.Change(); got != 8_500_000 {
		t.Fatalf("change = %v, want 8.5tr", got)
	}
	if got := series.ChangePercent(); got < 6.2 || got > 6.3 {
		t.Fatalf("change%% = %v, want about 6.27", got)
	}
	if !series.TwoSided() {
		t.Fatal("series reports one-sided")
	}
}

// Asking for a code the payload doesn't have must fail, not return whichever
// series happened to be inside.
func TestParseGoldHistoryRejectsAMismatchedCode(t *testing.T) {
	body, err := os.ReadFile("testdata/history_dohnl.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.ParseGoldHistory(body, "SJL1L10", "vang.today"); err == nil {
		t.Fatal("accepted a payload for a different gold type")
	}
}
