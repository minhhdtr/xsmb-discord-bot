// Package bot renders results and wires them to Discord.
package bot

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// Colours distinguish an answer to a command from the scheduled announcement.
const (
	colourQuery    = 0x2b6cb0
	colourAnnounce = 0xd69e2e
	colourProblem  = 0x9b2c2c
)

// Row headings. The full domain labels are too long for a phone.
var shortLabels = [domain.PrizeGroups]string{
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
				heading = shortLabels[tier]
			}
			b.WriteString(padRunes(heading, labelWidth))
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

// DrawEmbed builds the message for one result. announced changes the colour
// and headline so a scheduled post stands out.
func DrawEmbed(draw domain.Draw, announced bool) *discordgo.MessageEmbed {
	colour, prefix := colourQuery, "🎲"
	if announced {
		colour, prefix = colourAnnounce, "🔔"
	}
	return &discordgo.MessageEmbed{
		Title: fmt.Sprintf("%s XSMB · %s %s", prefix,
			domain.WeekdayVN(draw.Date), domain.FormatVN(draw.Date)),
		Description: fmt.Sprintf("**Đặc biệt · %s**\n```\n%s\n```",
			draw.Prizes.Special(), Table(draw.Prizes)),
		Color: colour,
		Fields: []*discordgo.MessageEmbedField{{
			Name:   "Đầu đuôi",
			Value:  "```\n" + HeadTail(draw.Prizes) + "\n```",
			Inline: false,
		}},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Nguồn %s · lấy lúc %s",
				draw.Source, draw.FetchedAt.In(domain.Location()).Format("15:04 02/01/2006")),
		},
	}
}

// NoticeEmbed reports anything that isn't a result.
func NoticeEmbed(title, body string, problem bool) *discordgo.MessageEmbed {
	colour := colourQuery
	if problem {
		colour = colourProblem
	}
	return &discordgo.MessageEmbed{Title: title, Description: body, Color: colour}
}

// padRunes pads by runes, not bytes - Vietnamese headings are multibyte and
// byte padding misaligns every row.
func padRunes(s string, width int) string {
	count := len([]rune(s))
	if count >= width {
		return s + " "
	}
	return s + strings.Repeat(" ", width-count)
}
