package bot

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/format"
)

const colourGold = 0xb7791f

// maxGoldFields keeps the embed under Discord's 25-field ceiling.
const maxGoldFields = 20

// GoldEmbed renders a board. Fields rather than a code block: a gold row is
// a label plus two numbers, which fits inline fields without truncation.
func GoldEmbed(board domain.GoldBoard) *discordgo.MessageEmbed {
	if !board.Valid() {
		return NoticeEmbed("Lỗi", "Không có dữ liệu giá vàng.", true)
	}

	embed := &discordgo.MessageEmbed{
		Title: "🥇 Giá vàng · " + updatedLabel(board),
		Color: colourGold,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Nguồn %s · mỗi lượng · lấy lúc %s",
				board.Source, board.FetchedAt.In(domain.Location()).Format("15:04 02/01/2006")),
		},
	}
	if world, ok := board.World(); ok {
		embed.Description = fmt.Sprintf("**%s** USD/oz  %s",
			format.Decimal(world.Buy, 1), changeLabel(world.ChangeBuy, 1))
	}

	for _, q := range board.Domestic() {
		if len(embed.Fields) >= maxGoldFields {
			break
		}
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   q.Name,
			Value:  goldValue(q),
			Inline: true,
		})
	}
	return embed
}

// goldValue is one dealer's cell.
func goldValue(q domain.GoldQuote) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Mua **%s**\nBán **%s**", format.Dong(q.Buy), format.Dong(q.Sell))
	// The two sides match in every row seen so far, but show both if they ever
	// disagree.
	if q.ChangeBuy != 0 || q.ChangeSell != 0 {
		if q.ChangeBuy == q.ChangeSell {
			fmt.Fprintf(&b, "\n%s", changeDong(q.ChangeBuy))
		} else {
			fmt.Fprintf(&b, "\nmua %s · bán %s", changeDong(q.ChangeBuy), changeDong(q.ChangeSell))
		}
	}
	return b.String()
}

func updatedLabel(board domain.GoldBoard) string {
	if board.UpdatedAt.IsZero() {
		return "không rõ thời điểm"
	}
	return board.UpdatedAt.In(domain.Location()).Format("15:04 02/01/2006")
}

// changeLabel renders a movement with an arrow. Upstream numbers carry float
// noise (7.599999999999454), so round first.
func changeLabel(delta float64, places int) string {
	rounded := format.Round(delta, places)
	switch {
	case rounded > 0:
		return "↑ " + format.Decimal(rounded, places)
	case rounded < 0:
		return "↓ " + format.Decimal(-rounded, places)
	default:
		return "→ 0"
	}
}

// changeDong renders a movement in đồng, matching the prices above it.
func changeDong(delta float64) string {
	switch rounded := format.Round(delta, 0); {
	case rounded > 0:
		return "↑ " + format.Dong(rounded)
	case rounded < 0:
		return "↓ " + format.Dong(-rounded)
	default:
		return "→ 0₫"
	}
}
