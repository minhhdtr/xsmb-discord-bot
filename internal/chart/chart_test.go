package chart_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/chart"
	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

func series(t *testing.T, count int, currency domain.Currency, twoSided bool) domain.GoldSeries {
	t.Helper()
	start := domain.NewDate(2026, 7, 23)
	points := make([]domain.GoldPoint, 0, count)
	for i := 0; i < count; i++ {
		buy := 143_000_000 + math.Sin(float64(i)/3)*800_000
		if currency == domain.USD {
			buy = 4500 + math.Sin(float64(i)/3)*60
		}
		p := domain.GoldPoint{Day: start.AddDate(0, 0, i), Buy: buy}
		if twoSided {
			p.Sell = buy + 3_000_000
		}
		points = append(points, p)
	}
	s, err := domain.NewGoldSeries("SJL1L10", "Vàng miếng SJC", currency, points)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func decode(t *testing.T, raw []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	return img
}

func TestRenderProducesADecodablePNG(t *testing.T) {
	raw, err := chart.Render(series(t, 30, domain.VND, true))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	img := decode(t, raw)
	if got := img.Bounds().Dx(); got != chart.Width {
		t.Fatalf("width = %d, want %d", got, chart.Width)
	}
	if got := img.Bounds().Dy(); got != chart.Height {
		t.Fatalf("height = %d, want %d", got, chart.Height)
	}
}

// countHues counts green and red pixels, so a test can check a line was drawn
// without pinning coordinates.
func countHues(img image.Image) (green, red int) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			switch {
			case c.G > 120 && c.G > c.R+40 && c.G > c.B+40:
				green++
			case c.R > 120 && c.R > c.G+40 && c.R > c.B+40:
				red++
			}
		}
	}
	return green, red
}

func TestTwoSidedSeriesDrawsBothLines(t *testing.T) {
	raw, err := chart.Render(series(t, 30, domain.VND, true))
	if err != nil {
		t.Fatal(err)
	}
	green, red := countHues(decode(t, raw))
	if green < 500 {
		t.Fatalf("only %d green pixels; the buy line is missing", green)
	}
	if red < 500 {
		t.Fatalf("only %d red pixels; the sell line is missing", red)
	}
}

// One line, and no "MUA" label - a spot price is not a dealer's buying quote.
func TestOneSidedSeriesDrawsOnlyTheBuyLine(t *testing.T) {
	raw, err := chart.Render(series(t, 30, domain.USD, false))
	if err != nil {
		t.Fatal(err)
	}
	green, red := countHues(decode(t, raw))
	if green < 500 {
		t.Fatalf("only %d green pixels", green)
	}
	if red > 50 {
		t.Fatalf("%d red pixels on a one-sided series; a legend or line leaked in", red)
	}
}

func TestRenderRejectsSeriesTooShortToDraw(t *testing.T) {
	one := series(t, 1, domain.VND, true)
	if _, err := chart.Render(one); !errors.Is(err, domain.ErrNoHistory) {
		t.Fatalf("err = %v, want ErrNoHistory", err)
	}
	var zero domain.GoldSeries
	if _, err := chart.Render(zero); !errors.Is(err, domain.ErrNoHistory) {
		t.Fatalf("err = %v, want ErrNoHistory", err)
	}
}

// A flat series still needs a readable axis, not a divide by zero.
func TestRenderHandlesAFlatSeries(t *testing.T) {
	points := make([]domain.GoldPoint, 0, 10)
	start := domain.NewDate(2026, 8, 1)
	for i := 0; i < 10; i++ {
		points = append(points, domain.GoldPoint{
			Day: start.AddDate(0, 0, i), Buy: 143_600_000, Sell: 146_600_000,
		})
	}
	s, err := domain.NewGoldSeries("FLAT", "Flat", domain.VND, points)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := chart.Render(s)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if green, _ := countHues(decode(t, raw)); green < 200 {
		t.Fatalf("flat series drew only %d green pixels", green)
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	s := series(t, 14, domain.VND, true)
	first, err := chart.Render(s)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		next, err := chart.Render(s)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, next) {
			t.Fatal("two renders of the same series differ")
		}
	}
}

func TestRenderCopesWithAWideRangeOfSizes(t *testing.T) {
	for _, count := range []int{2, 3, 7, 15, 30} {
		if _, err := chart.Render(series(t, count, domain.VND, true)); err != nil {
			t.Fatalf("%d points: %v", count, err)
		}
	}
}

func TestRenderStaysSmallEnoughForDiscord(t *testing.T) {
	raw, err := chart.Render(series(t, 30, domain.VND, true))
	if err != nil {
		t.Fatal(err)
	}
	// Discord's free-tier attachment ceiling is 8 MB; a chart should be tiny.
	if len(raw) > 200*1024 {
		t.Fatalf("chart is %d bytes", len(raw))
	}
}

var _ = time.Now
