// Package bot renders results and wires them to Discord.
package bot

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/present"
)

// Colours distinguish an answer to a command from the scheduled announcement.
const (
	colourQuery    = 0x2b6cb0
	colourAnnounce = 0xd69e2e
	colourProblem  = 0x9b2c2c
)

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
			draw.Prizes.Special(), present.Table(draw.Prizes)),
		Color: colour,
		Fields: []*discordgo.MessageEmbedField{{
			Name:   "Đầu đuôi",
			Value:  "```\n" + present.HeadTail(draw.Prizes) + "\n```",
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
