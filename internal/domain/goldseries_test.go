package domain_test

import (
	"errors"
	"testing"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

func points(days int) []domain.GoldPoint {
	start := domain.NewDate(2026, 8, 1)
	out := make([]domain.GoldPoint, 0, days)
	for i := 0; i < days; i++ {
		out = append(out, domain.GoldPoint{
			Day:  start.AddDate(0, 0, i),
			Buy:  143_000_000 + float64(i)*100_000,
			Sell: 146_000_000 + float64(i)*100_000,
		})
	}
	return out
}

// The chart walks the slice directly, so an unsorted series would draw a line
// going backwards through time and still look plausible.
func TestNewGoldSeriesSortsByDay(t *testing.T) {
	shuffled := points(5)
	shuffled[0], shuffled[4] = shuffled[4], shuffled[0]
	shuffled[1], shuffled[3] = shuffled[3], shuffled[1]

	series, err := domain.NewGoldSeries("SJL1L10", "SJC", domain.VND, shuffled)
	if err != nil {
		t.Fatal(err)
	}
	got := series.Points()
	for i := 1; i < len(got); i++ {
		if !got[i].Day.After(got[i-1].Day) {
			t.Fatalf("point %d (%s) does not follow %s", i,
				domain.FormatVN(got[i].Day), domain.FormatVN(got[i-1].Day))
		}
	}
}

func TestNewGoldSeriesDeduplicatesDays(t *testing.T) {
	dupes := append(points(3), domain.GoldPoint{
		Day: domain.NewDate(2026, 8, 2), Buy: 999_000_000, Sell: 999_000_000,
	})
	series, err := domain.NewGoldSeries("X", "X", domain.VND, dupes)
	if err != nil {
		t.Fatal(err)
	}
	if series.Len() != 3 {
		t.Fatalf("got %d points, want 3", series.Len())
	}
	// The later value for a repeated day wins.
	if series.Points()[1].Buy != 999_000_000 {
		t.Fatalf("duplicate day kept %v", series.Points()[1].Buy)
	}
}

func TestNewGoldSeriesRejectsBadInput(t *testing.T) {
	if _, err := domain.NewGoldSeries("", "X", domain.VND, points(3)); err == nil {
		t.Fatal("accepted an empty code")
	}
	if _, err := domain.NewGoldSeries("X", "X", domain.VND, nil); !errors.Is(err, domain.ErrNoHistory) {
		t.Fatalf("err = %v, want ErrNoHistory", err)
	}
	bad := points(3)
	bad[1].Buy = 0
	if _, err := domain.NewGoldSeries("X", "X", domain.VND, bad); err == nil {
		t.Fatal("accepted a zero buy price")
	}
	swapped := points(3)
	swapped[2].Sell = swapped[2].Buy - 1
	if _, err := domain.NewGoldSeries("X", "X", domain.VND, swapped); err == nil {
		t.Fatal("accepted a sell price below buy")
	}
}

func TestGoldSeriesRangeAndChange(t *testing.T) {
	series, err := domain.NewGoldSeries("X", "X", domain.VND, points(5))
	if err != nil {
		t.Fatal(err)
	}
	low, high := series.Range()
	if low != 143_000_000 || high != 146_400_000 {
		t.Fatalf("range = %v..%v", low, high)
	}
	if series.Change() != 400_000 {
		t.Fatalf("change = %v", series.Change())
	}
	if got := series.ChangePercent(); got < 0.27 || got > 0.29 {
		t.Fatalf("change%% = %v", got)
	}
	if !series.TwoSided() {
		t.Fatal("series with sell prices reports one-sided")
	}
}

// Range must ignore the missing sell side rather than pull the axis to zero.
func TestGoldSeriesIgnoresMissingSellSide(t *testing.T) {
	oneSided := points(4)
	for i := range oneSided {
		oneSided[i].Sell = 0
	}
	series, err := domain.NewGoldSeries("XAUUSD", "World", domain.USD, oneSided)
	if err != nil {
		t.Fatal(err)
	}
	if series.TwoSided() {
		t.Fatal("reports two-sided")
	}
	if low, _ := series.Range(); low != 143_000_000 {
		t.Fatalf("low = %v; a zero sell price leaked into the range", low)
	}
}

func TestZeroGoldSeriesIsInert(t *testing.T) {
	var zero domain.GoldSeries
	if zero.Valid() || zero.Len() != 0 || len(zero.Points()) != 0 {
		t.Fatal("zero series returned data")
	}
	if zero.Change() != 0 || zero.ChangePercent() != 0 || zero.TwoSided() {
		t.Fatal("zero series reported movement")
	}
	if low, high := zero.Range(); low != 0 || high != 0 {
		t.Fatalf("range = %v..%v", low, high)
	}
	if zero.First().Buy != 0 || zero.Last().Buy != 0 {
		t.Fatal("zero series returned endpoints")
	}
}
