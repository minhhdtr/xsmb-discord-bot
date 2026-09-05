package present_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/present"
)

func samplePrizes(t *testing.T) domain.Prizes {
	t.Helper()
	numbers := make([]string, 0, domain.TotalNumbers)
	for _, spec := range domain.PrizeLayout {
		for n := 0; n < spec.Count; n++ {
			numbers = append(numbers, fmt.Sprintf("%0*d", spec.Digits, n+1))
		}
	}
	p, err := domain.NewPrizes(numbers)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTableAlignsVietnameseHeadings(t *testing.T) {
	lines := strings.Split(present.Table(samplePrizes(t)), "\n")
	if len(lines) != 10 { // 8 tiers, with prize3 and prize5 each wrapping onto a second row
		t.Fatalf("got %d lines:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	// Every row must start its numbers at the same rune offset or the columns
	// skew on a phone. Byte offsets differ because of the diacritics.
	want := -1
	for _, line := range lines {
		offset := strings.IndexFunc(line, func(r rune) bool { return r >= '0' && r <= '9' })
		runeOffset := len([]rune(line[:offset]))
		if want == -1 {
			want = runeOffset
		}
		if runeOffset != want {
			t.Fatalf("row %q starts numbers at rune %d, want %d", line, runeOffset, want)
		}
	}
	if !strings.HasPrefix(lines[0], "Đặc biệt") {
		t.Fatalf("first row = %q", lines[0])
	}
}

func TestTableIsEmptyForInvalidPrizes(t *testing.T) {
	var zero domain.Prizes
	if present.Table(zero) != "" || present.HeadTail(zero) != "" {
		t.Fatal("rendered a zero-value Prizes")
	}
}

func TestHeadTailCoversEveryHead(t *testing.T) {
	lines := strings.Split(present.HeadTail(samplePrizes(t)), "\n")
	if len(lines) != 10 {
		t.Fatalf("got %d rows, want 10", len(lines))
	}
	total := 0
	for _, line := range lines {
		_, tails, _ := strings.Cut(line, "│")
		tails = strings.TrimSpace(tails)
		if tails == "-" {
			continue
		}
		total += len(strings.Fields(tails))
	}
	if total != domain.TotalNumbers {
		t.Fatalf("rows hold %d tails, want 27", total)
	}
}
