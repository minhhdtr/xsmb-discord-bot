package present

import (
	"fmt"
	"math"
	"strings"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// barWidth is the widest bar in a grouped frequency, chosen so a whole row
// fits a phone line without wrapping.
const barWidth = 12

// DigitCounts draws the đầu and đuôi counts of one draw as a small grid.
//
//	      0  1  2  3  4  5  6  7  8  9
//	Đầu   3  3  2  4  5  2  2  4  1  1
//	Đuôi  1  2  0  4  4  2  6  4  3  1
func DigitCounts(heads, tails [10]int) string {
	var b strings.Builder
	b.WriteString("      0  1  2  3  4  5  6  7  8  9\n")
	b.WriteString(digitRow("Đầu", heads))
	b.WriteString(digitRow("Đuôi", tails))
	return strings.TrimRight(b.String(), "\n")
}

func digitRow(label string, counts [10]int) string {
	var b strings.Builder
	b.WriteString(padRunes(label, 5))
	for _, n := range counts {
		fmt.Fprintf(&b, "%2d ", n)
	}
	return strings.TrimRight(b.String(), " ") + "\n"
}

// Buckets draws a grouped frequency: the count, a bar scaled to the busiest
// bucket, and how far the bucket sits from an even split.
//
// The percentage is the point of the table. A bucket holding 207 means nothing
// on its own; 15% below even means something immediately.
func Buckets(grouped domain.GroupedFrequency) string {
	peak := grouped.Peak()
	var b strings.Builder
	for _, bucket := range grouped.Buckets {
		bar := 0
		if peak > 0 {
			bar = bucket.Hits * barWidth / peak
		}
		fmt.Fprintf(&b, "%d │ %5d  %-*s %5s\n",
			bucket.Digit, bucket.Hits, barWidth, strings.Repeat("▇", bar),
			shareLabel(share(bucket.Hits, grouped.Even)))
	}
	return strings.TrimRight(b.String(), "\n")
}

// share is how far a bucket sits from an even split, in percent.
func share(hits int, even float64) float64 {
	if even == 0 {
		return 0
	}
	return (float64(hits) - even) / even * 100
}

// shareLabel shows which side of even a bucket sits on. Half a percent below
// even is not "-0%", it is level.
func shareLabel(value float64) string {
	rounded := math.Round(value)
	if rounded == 0 {
		return "0%"
	}
	return fmt.Sprintf("%+.0f%%", rounded)
}

// SpecialMonth lays a month of special prizes into two columns, which halves
// the height. Reading order is down the left column then down the right.
func SpecialMonth(days []domain.SpecialDay) string {
	if len(days) == 0 {
		return ""
	}
	half := (len(days) + 1) / 2
	var b strings.Builder
	b.WriteString("Ngày ĐB    Đề   Ngày ĐB    Đề\n")
	for i := 0; i < half; i++ {
		b.WriteString(specialCell(days[i]))
		if j := i + half; j < len(days) {
			b.WriteString("  " + specialCell(days[j]))
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func specialCell(d domain.SpecialDay) string {
	return fmt.Sprintf("%02d   %-6s %s", d.Day.Day(), d.Special, d.De)
}
