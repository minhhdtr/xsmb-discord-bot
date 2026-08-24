package bot_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/bot"
	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/service"
	"github.com/minhhdtr/xsmb-discord-bot/internal/storage"
)

// fillerTail pads a draw out to 27 prizes. Cycling the given tails instead
// would multiply each one by about fourteen and make hit counts unreadable.
const fillerTail = "99"

// drawTailed builds a draw whose first prizes end in the given tails, padded
// with fillerTail.
func drawTailed(t *testing.T, day time.Time, tails ...string) domain.Draw {
	t.Helper()
	numbers := make([]string, 0, domain.TotalNumbers)
	at := 0
	for _, spec := range domain.PrizeLayout {
		for n := 0; n < spec.Count; n++ {
			tail := fillerTail
			if at < len(tails) {
				tail = tails[at]
			}
			value := tail
			for len(value) < spec.Digits {
				value = "0" + value
			}
			numbers = append(numbers, value)
			at++
		}
	}
	prizes, err := domain.NewPrizes(numbers)
	if err != nil {
		t.Fatal(err)
	}
	return domain.Draw{Date: domain.DayOf(day), Prizes: prizes, Source: "test"}
}

// statsRouter seeds an archive and returns a router over it.
func statsRouter(t *testing.T, perDay [][]string) *bot.Router {
	t.Helper()
	store := storage.NewMemory()
	ctx := context.Background()
	start := domain.NewDate(2026, 8, 1)
	for i, tails := range perDay {
		if err := store.SaveDraw(ctx, drawTailed(t, start.AddDate(0, 0, i), tails...)); err != nil {
			t.Fatal(err)
		}
	}
	svc := service.New(store, &stubProvider{answer: found(t)},
		func() time.Time { return time.Date(2026, 9, 1, 20, 0, 0, 0, domain.Location()) }, quiet())
	return bot.NewRouter(svc, nil, store, "!xsmb", "!gold", quiet())
}

func text(t *testing.T, e *bot.Router, args ...string) string {
	t.Helper()
	embed := e.Handle(context.Background(), bot.Request{Args: args}).Embed
	var b strings.Builder
	b.WriteString(embed.Title + "\n" + embed.Description + "\n")
	for _, f := range embed.Fields {
		b.WriteString(f.Name + "\n" + f.Value + "\n")
	}
	if embed.Footer != nil {
		b.WriteString(embed.Footer.Text)
	}
	return b.String()
}

func TestLoGanCommand(t *testing.T) {
	r := statsRouter(t, [][]string{
		{"01", "02"}, {"02", "03"}, {"03", "04"}, {"04", "05"},
	})
	out := text(t, r, "logan")
	if !strings.Contains(out, "Lô gan") {
		t.Fatalf("title missing:\n%s", out)
	}
	// 01 last appeared on the first of four days, so it leads the ranking.
	lines := strings.Split(out, "\n")
	var first string
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "01") {
			first = l
			break
		}
	}
	if first == "" {
		t.Fatalf("01 missing from the ranking:\n%s", out)
	}
	if !strings.Contains(out, "Thống kê mô tả") {
		t.Fatalf("footer disclaimer missing:\n%s", out)
	}
}

func TestDeGanCommandUsesOnlyTheSpecialPrize(t *testing.T) {
	// "77" is never the special prize; the board's first tail is.
	r := statsRouter(t, [][]string{
		{"11", "77"}, {"22", "77"}, {"33", "77"},
	})
	out := text(t, r, "degan")
	if !strings.Contains(out, "Đề gan") {
		t.Fatalf("title = %q", out)
	}
	if strings.Contains(out, "77") {
		t.Fatalf("a lesser prize leaked into đề gan:\n%s", out)
	}
}

func TestTanSoCommandAndWindow(t *testing.T) {
	r := statsRouter(t, [][]string{{"01"}, {"02"}, {"03"}})
	out := text(t, r, "tanso")
	if !strings.Contains(out, "30 ngày") || !strings.Contains(out, "Về nhiều nhất") {
		t.Fatalf("default window wrong:\n%s", out)
	}
	if !strings.Contains(out, "nháy") {
		t.Fatalf("frequency unit missing:\n%s", out)
	}
	if custom := text(t, r, "tanso", "90"); !strings.Contains(custom, "90 ngày") {
		t.Fatalf("custom window ignored:\n%s", custom)
	}
	if bad := text(t, r, "tanso", "abc"); !strings.Contains(bad, "Không đọc được số ngày") {
		t.Fatalf("bad window accepted:\n%s", bad)
	}
}

func TestLoProfileCommand(t *testing.T) {
	r := statsRouter(t, [][]string{{"88"}, {"11"}, {"88"}})
	out := text(t, r, "lo", "88")
	if !strings.Contains(out, "Lô 88") {
		t.Fatalf("title = %q", out)
	}
	for _, want := range []string{"Lần cuối về", "Đang gan", "Chu kỳ TB", "Toàn kho"} {
		if !strings.Contains(out, want) {
			t.Fatalf("%q missing:\n%s", want, out)
		}
	}
	// A single digit is padded rather than rejected.
	if padded := text(t, r, "lo", "8"); !strings.Contains(padded, "Lô 08") {
		t.Fatalf("single digit not padded:\n%s", padded)
	}
	if missing := text(t, r, "lo"); !strings.Contains(missing, "Thiếu số") {
		t.Fatalf("missing argument accepted:\n%s", missing)
	}
	if bad := text(t, r, "lo", "123"); !strings.Contains(bad, "Không đọc được số") {
		t.Fatalf("three digits accepted:\n%s", bad)
	}
}

func TestLoProfileOfANumberNeverSeen(t *testing.T) {
	r := statsRouter(t, [][]string{{"01"}, {"02"}})
	out := text(t, r, "lo", "55")
	if !strings.Contains(out, "Chưa từng về") {
		t.Fatalf("out = %s", out)
	}
}

func TestSpecialMonthCommand(t *testing.T) {
	r := statsRouter(t, [][]string{{"11"}, {"22"}, {"33"}})
	out := text(t, r, "db", "08/2026")
	if !strings.Contains(out, "tháng 08/2026") {
		t.Fatalf("title = %q", out)
	}
	if !strings.Contains(out, "Ngày") || !strings.Contains(out, "Đề") {
		t.Fatalf("table header missing:\n%s", out)
	}
	if empty := text(t, r, "db", "03/2011"); !strings.Contains(empty, "Không có dữ liệu") {
		t.Fatalf("empty month:\n%s", empty)
	}
	if bad := text(t, r, "db", "abc"); !strings.Contains(bad, "Không đọc được tháng") {
		t.Fatalf("bad month accepted:\n%s", bad)
	}
}

// Every statistics embed must fit Discord's limits, month table included.
func TestStatsEmbedsFitDiscordLimits(t *testing.T) {
	perDay := make([][]string, 31)
	for i := range perDay {
		perDay[i] = []string{fmt.Sprintf("%02d", i%100), fmt.Sprintf("%02d", (i*7)%100)}
	}
	r := statsRouter(t, perDay)

	for _, args := range [][]string{
		{"logan"}, {"degan"}, {"tanso"}, {"lo", "88"}, {"db", "08/2026"},
	} {
		embed := r.Handle(context.Background(), bot.Request{Args: args}).Embed
		total := len(embed.Title) + len(embed.Description)
		if len(embed.Description) > 4096 {
			t.Fatalf("%v: description is %d chars", args, len(embed.Description))
		}
		for _, f := range embed.Fields {
			if len(f.Value) > 1024 {
				t.Fatalf("%v: field %q is %d chars", args, f.Name, len(f.Value))
			}
			total += len(f.Name) + len(f.Value)
		}
		if embed.Footer != nil {
			total += len(embed.Footer.Text)
		}
		if total > 6000 {
			t.Fatalf("%v: embed is %d chars", args, total)
		}
	}
}

// The old aliases now read as statistics commands.
func TestThongKeNoLongerMeansArchiveStatus(t *testing.T) {
	r := statsRouter(t, [][]string{{"01"}})
	if out := text(t, r, "status"); !strings.Contains(out, "Tình trạng") {
		t.Fatalf("status = %s", out)
	}
	for _, alias := range []string{"stats", "thongke"} {
		if out := text(t, r, alias); strings.Contains(out, "Tình trạng") {
			t.Fatalf("%q still reaches the archive summary:\n%s", alias, out)
		}
	}
}

func TestLoProfileShowsTheRecentStrip(t *testing.T) {
	// "88" twice on day one, once on day three, nothing after.
	r := statsRouter(t, [][]string{
		{"88", "88"}, {"11"}, {"88"}, {"12"}, {"13"},
	})
	out := text(t, r, "lo", "88")

	if !strings.Contains(out, "2·1··") {
		t.Fatalf("strip missing or wrong:\n%s", out)
	}
	if !strings.Contains(out, "5 kỳ gần nhất") {
		t.Fatalf("strip has no heading:\n%s", out)
	}
	if !strings.Contains(out, "01/08/2026 → 05/08/2026") {
		t.Fatalf("strip range wrong:\n%s", out)
	}
	if !strings.Contains(out, "số nháy") {
		t.Fatalf("legend missing:\n%s", out)
	}
}

func TestRecentStripIsOneCharacterPerDraw(t *testing.T) {
	perDay := make([][]string, 30)
	for i := range perDay {
		perDay[i] = []string{fmt.Sprintf("%02d", i)}
	}
	r := statsRouter(t, perDay)
	embed := r.Handle(context.Background(), bot.Request{Args: []string{"lo", "07"}}).Embed

	var strip string
	for _, f := range embed.Fields {
		if strings.Contains(f.Name, "kỳ gần nhất") {
			_, body, _ := strings.Cut(f.Value, "```\n")
			strip, _, _ = strings.Cut(body, "\n")
		}
	}
	if got := len([]rune(strip)); got != 30 {
		t.Fatalf("strip is %d characters for 30 draws: %q", got, strip)
	}
	// 07 was drawn on exactly one of those days.
	if strings.Count(strip, "1") != 1 || strings.Count(strip, "·") != 29 {
		t.Fatalf("strip = %q", strip)
	}
}

func TestProfileWithoutAnArchiveHasNoStrip(t *testing.T) {
	r := statsRouter(t, nil)
	embed := r.Handle(context.Background(), bot.Request{Args: []string{"lo", "88"}}).Embed
	for _, f := range embed.Fields {
		if strings.Contains(f.Name, "kỳ gần nhất") {
			t.Fatal("empty archive rendered a strip")
		}
	}
}
