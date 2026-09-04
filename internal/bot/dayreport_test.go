package bot_test

import (
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/minhhdtr/xsmb-discord-bot/internal/bot"
	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// reportDraw builds a draw from 27 known tails, so the embed can be checked
// against counts worked out by hand.
func reportDraw(t *testing.T, tails []string) domain.Draw {
	t.Helper()
	numbers := make([]string, 0, domain.TotalNumbers)
	at := 0
	for _, spec := range domain.PrizeLayout {
		for n := 0; n < spec.Count; n++ {
			padded := tails[at]
			for len(padded) < spec.Digits {
				padded = "1" + padded
			}
			numbers = append(numbers, padded)
			at++
		}
	}
	prizes, err := domain.NewPrizes(numbers)
	if err != nil {
		t.Fatal(err)
	}
	return domain.Draw{
		Date: domain.NewDate(2026, 9, 3), Prizes: prizes, Source: "fake",
		FetchedAt: time.Now().In(domain.Location()),
	}
}

func fieldValue(t *testing.T, embed *discordgo.MessageEmbed, name string) string {
	t.Helper()
	for _, f := range embed.Fields {
		if f.Name == name {
			return f.Value
		}
	}
	t.Fatalf("no field named %q", name)
	return ""
}

func TestDayReportEmbedShowsKepNhayAndMutes(t *testing.T) {
	tails := []string{
		"47", "33", "33", "33", "12", "12", "88", "05", "16",
		"21", "34", "45", "56", "67", "78", "89", "90", "01",
		"23", "36", "49", "52", "65", "70", "81", "94", "07",
	}
	embed := bot.DayReportEmbed(reportDraw(t, tails))

	if !strings.Contains(embed.Description, "Đề · 47") {
		t.Errorf("description is missing the special tail:\n%s", embed.Description)
	}
	if got := fieldValue(t, embed, "Lô kép"); got != "33 88" {
		t.Errorf("Lô kép = %q, want \"33 88\"", got)
	}
	if got := fieldValue(t, embed, "Nháy"); !strings.Contains(got, "33 (ba nháy)") ||
		!strings.Contains(got, "12 (hai nháy)") {
		t.Errorf("Nháy = %q", got)
	}
	if got := fieldValue(t, embed, "Câm"); !strings.Contains(got, "—") {
		t.Errorf("Câm = %q, want a dash for the empty side", got)
	}
}

// An incomplete draw cannot be analysed, and must not render an embed full of
// zeroes that reads like a real result.
func TestDayReportEmbedRefusesAnIncompleteDraw(t *testing.T) {
	embed := bot.DayReportEmbed(domain.Draw{Date: domain.NewDate(2026, 9, 3)})
	if !strings.Contains(embed.Title, "Không có dữ liệu") {
		t.Errorf("title = %q", embed.Title)
	}
}

func TestSlashNgayMapsToTheRouter(t *testing.T) {
	data := discordgo.ApplicationCommandInteractionData{
		Name: "thongke",
		Options: []*discordgo.ApplicationCommandInteractionDataOption{{
			Name: "ngay",
			Type: discordgo.ApplicationCommandOptionSubCommand,
			Options: []*discordgo.ApplicationCommandInteractionDataOption{{
				Name: "ngay", Type: discordgo.ApplicationCommandOptionString,
				Value: "03/09/2026",
			}},
		}},
	}
	kind, args, _ := bot.SlashRequest(data)
	if kind != bot.KindLottery {
		t.Fatalf("kind = %v", kind)
	}
	if want := []string{"ngay", "03/09/2026"}; strings.Join(args, " ") != strings.Join(want, " ") {
		t.Errorf("args = %v, want %v", args, want)
	}
}
