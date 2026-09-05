package bot

import (
	"context"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/present"
)

// colourSpin is deliberately unlike every other embed in the bot. A mock board
// holds 27 numbers laid out exactly like a real one, so the colour and the
// heading are what stop a screenshot being mistaken for a result.
const colourSpin = 0x6b46c1

// spinPace is the gap between reveals.
//
// Discord allows roughly five message edits per five seconds in a channel, so
// one a second sits right on the limit. Backing off to 1.2s keeps a margin,
// and a spin that takes about half a minute reads better than one that
// scrolls past. discordgo queues against the limit rather than failing, so
// overrunning would only slow the spin down - but a queue that deep would
// hold up every other command too.
//
// Discord say not to hard code their limits, since they can change; this is
// the one place to adjust if the pace starts to fight the API.
const spinPace = 1200 * time.Millisecond

// spinLabel goes in the heading of every frame.
const spinLabel = "QUAY THỬ"

// quayThu builds a whole spin up front. Nothing here reaches the service: a
// spin is invented, so it must not touch the archive, and the only way to be
// sure of that is for the board to come from an endpoint that stores nothing.
func (r *Router) quayThu(ctx context.Context, channelID string) Reply {
	if until, busy := r.spinning(channelID); busy {
		return embed(NoticeEmbed("Đang quay",
			fmt.Sprintf("Kênh này đang có một lượt quay. Thử lại sau %d giây nhé.",
				int(time.Until(until).Seconds())+1), false))
	}

	spin, err := r.core.Spin(ctx)
	if err != nil {
		r.log.Error("spin failed", "error", err)
		return embed(NoticeEmbed("Lỗi", "Không quay được. Thử lại sau nhé.", true))
	}
	frames := make([]*discordgo.MessageEmbed, 0, spin.Steps()+1)
	for step := 0; step <= spin.Steps(); step++ {
		frames = append(frames, SpinEmbed(spin, step))
	}
	return Reply{Embed: frames[0], Frames: frames[1:], Pace: spinPace}
}

// spinning reports whether a spin is already running in this channel, and when
// it should be done. The reservation expires on its own, so nothing has to
// hand a lock back across the layer that does the waiting.
func (r *Router) spinning(channelID string) (time.Time, bool) {
	r.spinMu.Lock()
	defer r.spinMu.Unlock()

	now := r.now()
	if until, ok := r.spins[channelID]; ok && now.Before(until) {
		return until, true
	}
	if r.spins == nil {
		r.spins = make(map[string]time.Time)
	}
	// One extra pace of slack, so the next spin cannot start into the tail of
	// this one's last edit.
	r.spins[channelID] = now.Add(time.Duration(domain.TotalNumbers+1) * spinPace)

	// The map only ever holds channels that have spun, but a long-lived bot in
	// many channels would still grow it, so drop what has expired.
	for id, until := range r.spins {
		if id != channelID && !now.Before(until) {
			delete(r.spins, id)
		}
	}
	return time.Time{}, false
}

// SpinEmbed renders a spin after step reveals. step 0 is the blank board.
func SpinEmbed(spin domain.Spin, step int) *discordgo.MessageEmbed {
	cells := spin.Board(step)

	headline := "Đang quay…"
	if at, ok := spin.Just(step); ok {
		headline = fmt.Sprintf("**%s** · %s", cellLabel(at), cells[at])
	}
	if step >= spin.Steps() {
		headline = fmt.Sprintf("**Đặc biệt · %s**", cells[0])
	}

	return &discordgo.MessageEmbed{
		Title:       "🎰 " + spinLabel,
		Color:       colourSpin,
		Description: fmt.Sprintf("%s\n```\n%s\n```", headline, present.TableOf(cells)),
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Số ngẫu nhiên, quay cho vui · %d/%d",
				min(step, spin.Steps()), spin.Steps()),
		},
	}
}

// cellLabel names the prize a flat index belongs to, for the running headline.
func cellLabel(at int) string {
	offset := 0
	for tier, spec := range domain.PrizeLayout {
		if at < offset+spec.Count {
			return present.ShortLabels[tier]
		}
		offset += spec.Count
	}
	return ""
}
