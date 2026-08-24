package provider_test

import (
	"strings"
	"testing"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/provider"
)

// The docs and the live endpoint disagree, so the decoder accepts several
// shapes. One case each.
func TestParseGoldHistoryAcceptsTheDocumentedShape(t *testing.T) {
	body := []byte(`{"success":true,"current_time":1787295606,"data":[
		{"type_code":"SJL1L10","buy":143600000,"sell":146600000,"update_time":1787295606},
		{"type_code":"SJL1L10","buy":143000000,"sell":146000000,"update_time":1787209206},
		{"type_code":"SJL1L10","buy":142500000,"sell":145500000,"update_time":1787122806}]}`)
	series, err := provider.ParseGoldHistory(body, "SJL1L10", "test")
	if err != nil {
		t.Fatalf("ParseGoldHistory: %v", err)
	}
	if series.Len() != 3 {
		t.Fatalf("got %d points", series.Len())
	}
	// Oldest first, whatever order the payload used.
	points := series.Points()
	if domain.FormatVN(points[0].Day) != "19/08/2026" {
		t.Fatalf("first point = %s", domain.FormatVN(points[0].Day))
	}
	if points[2].Buy != 143600000 {
		t.Fatalf("last buy = %v", points[2].Buy)
	}
	if series.Name != "Vàng miếng SJC" {
		t.Fatalf("name = %q, want the Vietnamese label", series.Name)
	}
}

func TestParseGoldHistoryAcceptsDateStrings(t *testing.T) {
	body := []byte(`{"success":true,"history":[
		{"date":"2026-08-19","buy":142500000,"sell":145500000},
		{"date":"2026-08-20","buy":143000000,"sell":146000000},
		{"date":"2026-08-21","buy":143600000,"sell":146600000}]}`)
	series, err := provider.ParseGoldHistory(body, "SJL1L10", "test")
	if err != nil {
		t.Fatalf("ParseGoldHistory: %v", err)
	}
	if series.Len() != 3 || domain.FormatVN(series.Last().Day) != "21/08/2026" {
		t.Fatalf("series = %d points ending %s", series.Len(), domain.FormatVN(series.Last().Day))
	}
}

func TestParseGoldHistoryAcceptsADateKeyedObject(t *testing.T) {
	body := []byte(`{"success":true,"prices":{
		"2026-08-19":{"buy":142500000,"sell":145500000},
		"2026-08-20":{"buy":143000000,"sell":146000000},
		"2026-08-21":{"buy":143600000,"sell":146600000}}}`)
	series, err := provider.ParseGoldHistory(body, "SJL1L10", "test")
	if err != nil {
		t.Fatalf("ParseGoldHistory: %v", err)
	}
	if series.Len() != 3 {
		t.Fatalf("got %d points", series.Len())
	}
}

func TestParseGoldHistoryAcceptsABareArray(t *testing.T) {
	body := []byte(`[{"date":"2026-08-20","buy":143000000,"sell":146000000},
	                 {"date":"2026-08-21","buy":143600000,"sell":146600000}]`)
	series, err := provider.ParseGoldHistory(body, "SJL1L10", "test")
	if err != nil {
		t.Fatalf("ParseGoldHistory: %v", err)
	}
	if series.Len() != 2 {
		t.Fatalf("got %d points", series.Len())
	}
}

// A payload carrying several types must be filtered down to the one asked for.
func TestParseGoldHistoryKeepsOnlyTheRequestedType(t *testing.T) {
	body := []byte(`{"success":true,"data":[
		{"type_code":"SJL1L10","buy":143600000,"sell":146600000,"update_time":1787295606},
		{"type_code":"SJ9999","buy":143100000,"sell":146100000,"update_time":1787295606},
		{"type_code":"SJL1L10","buy":143000000,"sell":146000000,"update_time":1787209206}]}`)
	series, err := provider.ParseGoldHistory(body, "SJL1L10", "test")
	if err != nil {
		t.Fatal(err)
	}
	if series.Len() != 2 {
		t.Fatalf("got %d points, want only the requested type", series.Len())
	}
}

func TestParseGoldHistoryHandlesTheOneSidedWorldSeries(t *testing.T) {
	body := []byte(`{"success":true,"data":[
		{"type_code":"XAUUSD","buy":4557.8,"sell":0,"update_time":1787209206},
		{"type_code":"XAUUSD","buy":4565.4,"sell":0,"update_time":1787295606}]}`)
	series, err := provider.ParseGoldHistory(body, "XAUUSD", "test")
	if err != nil {
		t.Fatal(err)
	}
	if series.Currency != domain.USD {
		t.Fatalf("currency = %q, want USD", series.Currency)
	}
	if series.TwoSided() {
		t.Fatal("world series reports a sell side")
	}
}

// The error must name the keys it saw, not just say "cannot parse".
func TestParseGoldHistoryReportsTheShapeItSaw(t *testing.T) {
	body := []byte(`{"success":true,"result":{"nope":1},"meta":{"x":2}}`)
	_, err := provider.ParseGoldHistory(body, "SJL1L10", "vang.today")
	if err == nil {
		t.Fatal("accepted an unknown shape")
	}
	for _, want := range []string{"meta", "result", "vang.today"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %s", err, want)
		}
	}
}

func TestParseGoldHistoryRejectsUnusablePayloads(t *testing.T) {
	cases := map[string]string{
		"success false":  `{"success":false,"data":[]}`,
		"empty":          ``,
		"not json":       `<html>nope</html>`,
		"no usable rows": `{"success":true,"data":[{"type_code":"SJL1L10","buy":0,"update_time":0}]}`,
	}
	for label, body := range cases {
		if _, err := provider.ParseGoldHistory([]byte(body), "SJL1L10", "test"); err == nil {
			t.Fatalf("%s: accepted", label)
		}
	}
}

func TestGoldCodesIsACopy(t *testing.T) {
	codes := provider.GoldCodes()
	if len(codes) == 0 || codes[0] != "XAUUSD" {
		t.Fatalf("codes = %v", codes)
	}
	codes[0] = "MUTATED"
	if provider.GoldCodes()[0] != "XAUUSD" {
		t.Fatal("GoldCodes returned the backing array")
	}
}
