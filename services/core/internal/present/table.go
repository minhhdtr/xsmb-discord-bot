package present

// Package present renders the monospace tables every client shows.
//
// This is domain knowledge, not chat knowledge. Which prize holds how many
// numbers of how many digits, how many fit on one phone line, how wide the
// heading column must be for the columns to line up - none of that is a
// Discord concept, and none of it changes if the next client is Telegram or a
// web page. It lives here so core can serve the drawn table beside the raw
// numbers, and so the column arithmetic exists in one place rather than once
// per client.
//
// The rune padding is the clearest reason to centralise it. Vietnamese
// headings are multibyte, byte padding misaligns every row, and the result
// looks like a rendering glitch rather than an encoding mistake.

import (
	"fmt"
	"strings"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// Row headings. The full domain labels are too long for a phone.
var ShortLabels = [domain.PrizeGroups]string{
	"Đặc biệt", "Nhất", "Nhì", "Ba", "Tư", "Năm", "Sáu", "Bảy",
}

// Numbers per line, so the widest row stays inside a code block on a phone.
var perRow = [domain.PrizeGroups]int{1, 1, 2, 3, 4, 3, 3, 4}

// labelWidth is the padded width of the heading column, in runes.
const labelWidth = 9

// Table renders the 27 numbers as aligned monospace text, without fences.
func Table(prizes domain.Prizes) string {
	if !prizes.Valid() {
		return ""
	}
	return TableOf(prizes.Numbers())
}

// TableOf lays out 27 cells the same way, without needing a valid Prizes. A
// blank cell becomes dots the width of the number that goes there, which is
// what lets a spin show a board that is only part drawn.
func TableOf(cells []string) string {
	if len(cells) != domain.TotalNumbers {
		return ""
	}
	filled := make([]string, len(cells))
	at := 0
	for _, spec := range domain.PrizeLayout {
		for n := 0; n < spec.Count; n++ {
			if cells[at] == "" {
				filled[at] = strings.Repeat("·", spec.Digits)
			} else {
				filled[at] = cells[at]
			}
			at++
		}
	}

	var b strings.Builder
	offset := 0
	for tier, spec := range domain.PrizeLayout {
		numbers := filled[offset : offset+spec.Count]
		offset += spec.Count
		for start, line := 0, 0; start < len(numbers); start, line = start+perRow[tier], line+1 {
			end := start + perRow[tier]
			if end > len(numbers) {
				end = len(numbers)
			}
			heading := ""
			if line == 0 {
				heading = ShortLabels[tier]
			}
			b.WriteString(PadRunes(heading, labelWidth))
			b.WriteString(strings.Join(numbers[start:end], "  "))
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// HeadTail renders the đầu đuôi summary: every tail bucketed by first digit.
func HeadTail(prizes domain.Prizes) string {
	if !prizes.Valid() {
		return ""
	}
	buckets := prizes.TailsByHead()
	var b strings.Builder
	for head, tails := range buckets {
		b.WriteString(fmt.Sprintf("%d │ ", head))
		if len(tails) == 0 {
			b.WriteString("-")
		} else {
			b.WriteString(strings.Join(tails, " "))
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// padRunes pads by runes, not bytes - Vietnamese headings are multibyte and
// byte padding misaligns every row.
func PadRunes(s string, width int) string {
	count := len([]rune(s))
	if count >= width {
		return s + " "
	}
	return s + strings.Repeat(" ", width-count)
}
