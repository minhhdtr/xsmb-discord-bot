// Package format prints numbers the Vietnamese way: a dot groups thousands,
// a comma introduces the fraction.
package format

import (
	"math"
	"strconv"
	"strings"
)

// Dong renders an amount in đồng, with the currency sign: 145500000 becomes
// "145.500.000₫".
func Dong(amount float64) string {
	return Decimal(Round(amount, 0), 0) + "₫"
}

// Million renders đồng as triệu, dropping a fraction that rounds away.
// Used on the chart axis, where a full đồng figure is too wide.
func Million(amount float64, places int) string {
	return Trim(Decimal(amount/1e6, places))
}

// Decimal groups the whole part and joins any fraction with a comma.
func Decimal(value float64, places int) string {
	if places < 0 {
		places = 0
	}
	text := strconv.FormatFloat(math.Abs(value), 'f', places, 64)
	whole, frac, _ := strings.Cut(text, ".")

	var b strings.Builder
	if value < 0 {
		b.WriteByte('-')
	}
	for i, digit := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(digit)
	}
	if frac != "" {
		b.WriteByte(',')
		b.WriteString(frac)
	}
	return b.String()
}

// Trim drops a fraction that is all zeros, so an axis reads "145" rather
// than "145,0" while still allowing "145,5".
func Trim(text string) string {
	whole, frac, found := strings.Cut(text, ",")
	if !found || strings.Trim(frac, "0") != "" {
		return text
	}
	return whole
}

// Round rounds half away from zero, which is what a reader expects and what
// strconv does not do: it rounds half to even, turning 2,5 into 2.
func Round(value float64, places int) float64 {
	scale := math.Pow(10, float64(places))
	return math.Round(value*scale) / scale
}
