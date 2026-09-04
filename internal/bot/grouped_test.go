package bot_test

import (
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/minhhdtr/xsmb-discord-bot/internal/bot"
	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

func TestGroupedFrequencyEmbedShowsEveryBucket(t *testing.T) {
	freq := []domain.Frequency{
		{Number: "12", Hits: 20}, {Number: "17", Hits: 10}, {Number: "42", Hits: 10},
	}
	embed := bot.GroupedFrequencyEmbed(
		domain.GroupFrequency(freq, domain.ByHead), 30, 7600)

	if !strings.Contains(embed.Title, "Tần suất đầu") {
		t.Errorf("title = %q", embed.Title)
	}
	// Ten rows, one per digit, even the empty ones - a missing digit reads as
	// an error rather than a zero.
	for digit := 0; digit <= 9; digit++ {
		if !strings.Contains(embed.Description, string(rune('0'+digit))+" │") {
			t.Errorf("no row for digit %d:\n%s", digit, embed.Description)
		}
	}
	if !strings.Contains(embed.Description, "Chia đều là 4 lần mỗi ô") {
		t.Errorf("missing the even-split note:\n%s", embed.Description)
	}
	// Đầu 1 holds 30 of 40, so it is well above even.
	if !strings.Contains(embed.Description, "+650%") {
		t.Errorf("share of đầu 1 is wrong:\n%s", embed.Description)
	}
}

// The overlap only applies to chạm, and has to be said or the total looks
// broken against the number of lô drawn.
func TestGroupedFrequencyEmbedExplainsTheTouchOverlap(t *testing.T) {
	freq := []domain.Frequency{{Number: "87", Hits: 4}}

	touch := bot.GroupedFrequencyEmbed(
		domain.GroupFrequency(freq, domain.ByTouch), 30, 100)
	if !strings.Contains(touch.Description, "chạm hai chữ số") {
		t.Errorf("chạm is missing the overlap note:\n%s", touch.Description)
	}

	head := bot.GroupedFrequencyEmbed(
		domain.GroupFrequency(freq, domain.ByHead), 30, 100)
	if strings.Contains(head.Description, "chạm hai chữ số") {
		t.Errorf("đầu should not carry the overlap note:\n%s", head.Description)
	}
}

func TestGroupedFrequencyEmbedHandlesAnEmptyArchive(t *testing.T) {
	embed := bot.GroupedFrequencyEmbed(
		domain.GroupFrequency(nil, domain.ByTail), 30, 0)
	if !strings.Contains(embed.Title, "Chưa có dữ liệu") {
		t.Errorf("title = %q", embed.Title)
	}
}

func TestSlashTanSoCarriesTheGrouping(t *testing.T) {
	sub := func(opts ...*discordgo.ApplicationCommandInteractionDataOption) discordgo.ApplicationCommandInteractionData {
		return discordgo.ApplicationCommandInteractionData{
			Name: "thongke",
			Options: []*discordgo.ApplicationCommandInteractionDataOption{{
				Name: "tanso", Type: discordgo.ApplicationCommandOptionSubCommand,
				Options: opts,
			}},
		}
	}
	days := &discordgo.ApplicationCommandInteractionDataOption{
		Name: "ngay", Type: discordgo.ApplicationCommandOptionInteger, Value: float64(90),
	}
	kind := &discordgo.ApplicationCommandInteractionDataOption{
		Name: "kieu", Type: discordgo.ApplicationCommandOptionString, Value: "cham",
	}

	for _, tc := range []struct {
		name string
		data discordgo.ApplicationCommandInteractionData
		want string
	}{
		{"cả hai", sub(days, kind), "tanso 90 cham"},
		{"chỉ kiểu", sub(kind), "tanso cham"},
		{"chỉ ngày", sub(days), "tanso 90"},
		{"không gì", sub(), "tanso"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, args, _ := bot.SlashRequest(tc.data)
			if got := strings.Join(args, " "); got != tc.want {
				t.Errorf("args = %q, want %q", got, tc.want)
			}
		})
	}
}

// The slash choices must be strings the router can parse back, or the
// dropdown offers an option that then fails.
func TestSlashTanSoChoicesParse(t *testing.T) {
	for _, c := range bot.SlashCommands(time.Now()) {
		if c.Name != "thongke" {
			continue
		}
		for _, sub := range c.Options {
			if sub.Name != "tanso" {
				continue
			}
			for _, opt := range sub.Options {
				if opt.Name != "kieu" {
					continue
				}
				if len(opt.Choices) == 0 {
					t.Fatal("kieu has no choices")
				}
				for _, choice := range opt.Choices {
					value, _ := choice.Value.(string)
					if _, err := domain.ParseGrouping(value); err != nil {
						t.Errorf("choice %q: %v", value, err)
					}
				}
			}
		}
	}
}
