package format_test

import (
	"testing"

	"github.com/minhhdtr/xsmb-discord-bot/internal/format"
)

func TestDong(t *testing.T) {
	cases := map[float64]string{
		145_500_000: "145.500.000₫",
		143_600_000: "143.600.000₫",
		4_565:       "4.565₫",
		0:           "0₫",
		-600_000:    "-600.000₫",
		1_234:       "1.234₫",
		999:         "999₫",
	}
	for value, want := range cases {
		if got := format.Dong(value); got != want {
			t.Fatalf("Dong(%v) = %q, want %q", value, got, want)
		}
	}
}

// The source reports fractional đồng for jewellery grades.
func TestDongRoundsFractionalDong(t *testing.T) {
	if got := format.Dong(135_574_257.4257); got != "135.574.257₫" {
		t.Fatalf("got %q", got)
	}
	if got := format.Dong(135_574_257.6); got != "135.574.258₫" {
		t.Fatalf("got %q", got)
	}
}

func TestMillion(t *testing.T) {
	cases := []struct {
		value  float64
		places int
		want   string
	}{
		{145_500_000, 1, "145,5"},
		{145_000_000, 1, "145"},
		{143_600_000, 1, "143,6"},
		{130_000_000, 1, "130"},
		{1_500_000_000, 1, "1.500"},
	}
	for _, c := range cases {
		if got := format.Million(c.value, c.places); got != c.want {
			t.Fatalf("Million(%v) = %q, want %q", c.value, got, c.want)
		}
	}
}

func TestDecimal(t *testing.T) {
	cases := []struct {
		value  float64
		places int
		want   string
	}{
		{4565.4, 1, "4.565,4"},
		{1000, 0, "1.000"},
		{999, 0, "999"},
		{-600, 0, "-600"},
		{1_234_567, 0, "1.234.567"},
		{7.599999999999454, 1, "7,6"},
		{0, 2, "0,00"},
	}
	for _, c := range cases {
		if got := format.Decimal(c.value, c.places); got != c.want {
			t.Fatalf("Decimal(%v,%d) = %q, want %q", c.value, c.places, got, c.want)
		}
	}
}

// strconv rounds half to even, so 2,5 would print as "2". Round must not.
func TestRoundGoesHalfAwayFromZero(t *testing.T) {
	cases := map[float64]float64{2.5: 3, 3.5: 4, -2.5: -3, 2.4: 2}
	for in, want := range cases {
		if got := format.Round(in, 0); got != want {
			t.Fatalf("Round(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestTrim(t *testing.T) {
	cases := map[string]string{"145,0": "145", "145,00": "145", "145,5": "145,5", "145": "145"}
	for in, want := range cases {
		if got := format.Trim(in); got != want {
			t.Fatalf("Trim(%q) = %q, want %q", in, got, want)
		}
	}
}
