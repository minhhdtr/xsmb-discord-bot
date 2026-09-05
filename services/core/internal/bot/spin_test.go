package bot_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/minhhdtr/xsmb-discord-bot/internal/bot"
	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// boardOf pulls the fenced table out of a spin embed's description.
func boardOf(t *testing.T, description string) string {
	t.Helper()
	parts := strings.Split(description, "```")
	if len(parts) < 3 {
		t.Fatalf("no code block in:\n%s", description)
	}
	return parts[1]
}

func fixedSpin() domain.Spin {
	n := 0
	return domain.NewSpin(func(bound int) int {
		n = (n*7919 + 104729) % 1000003
		return n % bound
	})
}

// A mock board is laid out exactly like a real one, so the framing is the only
// thing keeping a screenshot of it from being read as a result. Every frame
// has to carry it, not just the last.
//
// Three marks share the job: the heading, the footer, and a colour used
// nowhere else. The heading is the shortest of them, so the other two are
// checked here too rather than taken on trust.
func TestSpinEmbedIsLabelledOnEveryFrame(t *testing.T) {
	spin := fixedSpin()
	for step := 0; step <= spin.Steps(); step++ {
		e := bot.SpinEmbed(spin, step)
		if !strings.Contains(e.Title, "QUAY THỬ") {
			t.Fatalf("step %d title = %q", step, e.Title)
		}
		if !strings.Contains(e.Footer.Text, "ngẫu nhiên") {
			t.Fatalf("step %d footer = %q", step, e.Footer.Text)
		}
	}
	// And it must not wear the colour of a real result.
	real := bot.DrawEmbed(domain.Draw{}, false)
	if bot.SpinEmbed(spin, 0).Color == real.Color {
		t.Error("a spin has the same colour as a query result")
	}
}

func TestSpinEmbedRevealsOneCellAtATime(t *testing.T) {
	spin := fixedSpin()
	previous := -1
	for step := 0; step <= spin.Steps(); step++ {
		// Only the board, not the headline - that uses · as a separator too.
		drawn := strings.Count(boardOf(t, bot.SpinEmbed(spin, step).Description), "·")
		if step == 0 && drawn == 0 {
			t.Fatal("the blank board has no placeholders")
		}
		if previous >= 0 && drawn >= previous {
			t.Fatalf("step %d did not reveal anything (placeholders %d -> %d)",
				step, previous, drawn)
		}
		previous = drawn
	}
	if previous != 0 {
		t.Errorf("the finished board still has %d placeholder characters", previous)
	}
}

func TestSpinEmbedHoldsTheSpecialBackUntilTheEnd(t *testing.T) {
	spin := fixedSpin()
	special := spin.Board(spin.Steps())[0]

	nearly := bot.SpinEmbed(spin, spin.Steps()-1)
	if strings.Contains(nearly.Description, special) {
		t.Errorf("the special leaked one step early:\n%s", nearly.Description)
	}
	last := bot.SpinEmbed(spin, spin.Steps())
	if !strings.Contains(last.Description, "Đặc biệt · "+special) {
		t.Errorf("the last frame does not announce the special:\n%s", last.Description)
	}
}

// Placeholders have to be the width of the number that goes there, or the
// columns jump about as the board fills.
func TestSpinEmbedKeepsColumnsSteady(t *testing.T) {
	spin := fixedSpin()
	widths := func(step int) []int {
		var out []int
		for _, line := range strings.Split(bot.SpinEmbed(spin, step).Description, "\n") {
			if strings.HasPrefix(line, "```") || line == "" || strings.HasPrefix(line, "**") ||
				strings.HasPrefix(line, "Đang quay") {
				continue
			}
			out = append(out, len([]rune(line)))
		}
		return out
	}
	want := widths(spin.Steps())
	for step := 0; step < spin.Steps(); step++ {
		got := widths(step)
		if len(got) != len(want) {
			t.Fatalf("step %d has %d board lines, want %d", step, len(got), len(want))
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("step %d line %d is %d runes wide, finished board is %d",
					step, i, got[i], want[i])
			}
		}
	}
}

func TestQuayThuBuildsEveryFrameAndLocksTheChannel(t *testing.T) {
	r := statsRouter(t, [][]string{{"01"}, {"02"}})

	first := r.Dispatch(context.Background(), bot.KindLottery,
		bot.Request{Args: []string{"quaythu"}, ChannelID: "chan-a"})
	if got := len(first.Frames); got != domain.TotalNumbers {
		t.Fatalf("got %d frames, want %d", got, domain.TotalNumbers)
	}
	if first.Pace <= 0 {
		t.Error("frames have no pace, so they would all fire at once")
	}
	if !strings.Contains(first.Embed.Description, "·") {
		t.Error("the first frame is not a blank board")
	}

	// A second spin in the same channel would interleave its edits with the
	// first and burn the channel's edit budget.
	second := r.Dispatch(context.Background(), bot.KindLottery,
		bot.Request{Args: []string{"quaythu"}, ChannelID: "chan-a"})
	if len(second.Frames) != 0 || !strings.Contains(second.Embed.Title, "Đang quay") {
		t.Errorf("a second spin was allowed in the same channel: %q", second.Embed.Title)
	}

	// Another channel is unaffected.
	other := r.Dispatch(context.Background(), bot.KindLottery,
		bot.Request{Args: []string{"quaythu"}, ChannelID: "chan-b"})
	if len(other.Frames) != domain.TotalNumbers {
		t.Errorf("a spin in another channel was blocked: %d frames", len(other.Frames))
	}
}

func TestSlashQuayThuMapsToTheRouter(t *testing.T) {
	kind, args, ephemeral := bot.SlashRequest(
		discordgo.ApplicationCommandInteractionData{Name: "quaythu"})
	if kind != bot.KindLottery {
		t.Fatalf("kind = %v", kind)
	}
	if strings.Join(args, " ") != "quaythu" {
		t.Errorf("args = %v", args)
	}
	// A spin is for the room, not for one person.
	if ephemeral {
		t.Error("the spin was made ephemeral")
	}
}

// The whole sequence has to fit inside a slash interaction token, which lasts
// fifteen minutes.
func TestSpinFitsTheInteractionWindow(t *testing.T) {
	r := statsRouter(t, [][]string{{"01"}})
	reply := r.Dispatch(context.Background(), bot.KindLottery,
		bot.Request{Args: []string{"quaythu"}, ChannelID: "chan-c"})

	total := time.Duration(len(reply.Frames)) * reply.Pace
	if total > 14*time.Minute {
		t.Errorf("a spin takes %s, too close to the 15 minute token", total)
	}
	if total < 10*time.Second {
		t.Errorf("a spin takes %s, too fast to watch", total)
	}
}
