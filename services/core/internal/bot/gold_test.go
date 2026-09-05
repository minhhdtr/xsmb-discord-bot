package bot_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/minhhdtr/xsmb-discord-bot/internal/bot"
	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/provider"
	"github.com/minhhdtr/xsmb-discord-bot/internal/service"
)

func goldBoard(t *testing.T) domain.GoldBoard {
	t.Helper()
	quotes := []domain.GoldQuote{
		{Code: "XAUUSD", Name: "Vàng thế giới", Buy: 4565.4, Sell: 0,
			ChangeBuy: 7.599999999999454, Currency: domain.USD},
		{Code: "SJL1L10", Name: "Vàng miếng SJC", Buy: 143_600_000, Sell: 146_600_000,
			ChangeBuy: 600_000, ChangeSell: 600_000, Currency: domain.VND},
		{Code: "DOJINHTV", Name: "DOJI nữ trang", Buy: 143_800_000, Sell: 147_800_000,
			ChangeBuy: -200_000, ChangeSell: -200_000, Currency: domain.VND},
		{Code: "ODD", Name: "Lệch hai chiều", Buy: 100_000_000, Sell: 101_000_000,
			ChangeBuy: 500_000, ChangeSell: -300_000, Currency: domain.VND},
	}
	board, err := domain.NewGoldBoard(quotes,
		time.Date(2026, 8, 21, 14, 0, 6, 0, domain.Location()), "vang.today",
		time.Date(2026, 8, 21, 14, 5, 0, 0, domain.Location()))
	if err != nil {
		t.Fatal(err)
	}
	return board
}

func TestGoldEmbedShape(t *testing.T) {
	embed := bot.GoldEmbed(goldBoard(t))

	if !strings.Contains(embed.Title, "14:00 21/08/2026") {
		t.Fatalf("title = %q, want the source's own timestamp", embed.Title)
	}
	// Three domestic rows become three fields; the world row is the summary.
	if len(embed.Fields) != 3 {
		t.Fatalf("got %d fields, want 3", len(embed.Fields))
	}
	if !strings.Contains(embed.Description, "4.565,4") {
		t.Fatalf("description = %q", embed.Description)
	}
	// Float noise (7.599999999999454) must be rounded away.
	if !strings.Contains(embed.Description, "↑ 7,6") || strings.Contains(embed.Description, "999") {
		t.Fatalf("description = %q", embed.Description)
	}
	if !strings.Contains(embed.Footer.Text, "mỗi lượng") {
		t.Fatalf("footer = %q", embed.Footer.Text)
	}
}

// Full đồng with the sign, so a lượng price can't be read as a chỉ price.
func TestGoldEmbedShowsFullDongWithCurrencySign(t *testing.T) {
	fields := bot.GoldEmbed(goldBoard(t)).Fields
	sjc := fields[0]
	if sjc.Name != "Vàng miếng SJC" {
		t.Fatalf("field name = %q", sjc.Name)
	}
	if !strings.Contains(sjc.Value, "143.600.000₫") || !strings.Contains(sjc.Value, "146.600.000₫") {
		t.Fatalf("value = %q", sjc.Value)
	}
	// The truncated forms must be gone entirely.
	if strings.Contains(sjc.Value, "143.600\n") || strings.Contains(sjc.Value, "**143.600**") {
		t.Fatalf("value still carries a nghìn figure: %q", sjc.Value)
	}
	if !strings.Contains(sjc.Value, "↑ 600.000₫") {
		t.Fatalf("value = %q, want the change in đồng", sjc.Value)
	}
	if !strings.Contains(fields[1].Value, "↓ 200.000₫") {
		t.Fatalf("falling row = %q", fields[1].Value)
	}
}

// The world row is quoted in USD per ounce and must not gain a đồng sign.
func TestGoldEmbedLeavesTheWorldRowInUSD(t *testing.T) {
	embed := bot.GoldEmbed(goldBoard(t))
	if strings.Contains(embed.Description, "₫") {
		t.Fatalf("world row carries a đồng sign: %q", embed.Description)
	}
	if !strings.Contains(embed.Description, "4.565,4") || !strings.Contains(embed.Description, "USD/oz") {
		t.Fatalf("description = %q", embed.Description)
	}
}

// Both sides shown when they disagree.
func TestGoldEmbedShowsBothSidesWhenTheyDisagree(t *testing.T) {
	value := bot.GoldEmbed(goldBoard(t)).Fields[2].Value
	if !strings.Contains(value, "mua ↑ 500") || !strings.Contains(value, "bán ↓ 300") {
		t.Fatalf("value = %q", value)
	}
}

func TestGoldEmbedRejectsAnEmptyBoard(t *testing.T) {
	var zero domain.GoldBoard
	if embed := bot.GoldEmbed(zero); !strings.Contains(embed.Title, "Lỗi") {
		t.Fatalf("title = %q", embed.Title)
	}
}

func TestGoldEmbedFitsDiscordLimits(t *testing.T) {
	embed := bot.GoldEmbed(goldBoard(t))
	total := len(embed.Title) + len(embed.Description) + len(embed.Footer.Text)
	for _, f := range embed.Fields {
		if len(f.Name) > 256 || len(f.Value) > 1024 {
			t.Fatalf("field %q too long", f.Name)
		}
		total += len(f.Name) + len(f.Value)
	}
	if total > 6000 {
		t.Fatalf("embed is %d chars, over Discord's 6000 limit", total)
	}
	if len(embed.Fields) > 25 {
		t.Fatalf("%d fields, over Discord's limit", len(embed.Fields))
	}
}

// --- routing between the two prefixes ---

func newGoldRouter(t *testing.T, board func(context.Context) (domain.GoldBoard, error)) *bot.Router {
	t.Helper()
	store := memStore()
	svc := service.New(store, &stubProvider{answer: found(t)}, func() time.Time { return at(20, 0) }, quiet())
	gold := service.NewGold(goldFunc(board), time.Minute, time.Hour, func() time.Time { return at(20, 0) }, quiet())
	return bot.NewRouter(svc, gold, store, "!xsmb", "!gold", quiet())
}

type goldFunc func(context.Context) (domain.GoldBoard, error)

func (g goldFunc) Name() string                                        { return "stub-gold" }
func (g goldFunc) Board(ctx context.Context) (domain.GoldBoard, error) { return g(ctx) }

func (g goldFunc) History(ctx context.Context, code string, days int) (domain.GoldSeries, error) {
	points := make([]domain.GoldPoint, 0, days)
	for i := 0; i < days; i++ {
		points = append(points, domain.GoldPoint{
			Day:  domain.NewDate(2026, 8, 21).AddDate(0, 0, i-days+1),
			Buy:  143_000_000 + float64(i)*100_000,
			Sell: 146_000_000 + float64(i)*100_000,
		})
	}
	return domain.NewGoldSeries(code, code, domain.VND, points)
}

var _ provider.GoldProvider = goldFunc(nil)

func TestMatchRoutesEachPrefix(t *testing.T) {
	r := newGoldRouter(t, func(context.Context) (domain.GoldBoard, error) { return goldBoard(t), nil })
	cases := []struct {
		content string
		want    bot.Kind
	}{
		{"!xsmb", bot.KindLottery},
		{"!xsmb 14/08/2026", bot.KindLottery},
		{"!gold", bot.KindGold},
		{"!GOLD", bot.KindGold},
		{"  !gold  ", bot.KindGold},
		{"!goldfinger", bot.KindNone},
		{"!xsmbgold", bot.KindNone},
		{"gold", bot.KindNone},
		{"", bot.KindNone},
	}
	for _, c := range cases {
		if kind, _ := r.Match(c.content); kind != c.want {
			t.Fatalf("Match(%q) = %v, want %v", c.content, kind, c.want)
		}
	}
}

func TestHandleGoldRendersAndReportsErrors(t *testing.T) {
	ok := newGoldRouter(t, func(context.Context) (domain.GoldBoard, error) { return goldBoard(t), nil })
	if embed := ok.HandleGold(context.Background(), bot.Request{}).Embed; !strings.Contains(embed.Title, "Giá vàng") {
		t.Fatalf("title = %q", embed.Title)
	}
	broken := newGoldRouter(t, func(context.Context) (domain.GoldBoard, error) {
		return domain.GoldBoard{}, errors.New("upstream down")
	})
	if embed := broken.HandleGold(context.Background(), bot.Request{}).Embed; !strings.Contains(embed.Title, "Lỗi") {
		t.Fatalf("title = %q", embed.Title)
	}
}

func TestHandleGoldWithoutConfiguration(t *testing.T) {
	store := memStore()
	svc := service.New(store, &stubProvider{answer: found(t)}, func() time.Time { return at(20, 0) }, quiet())
	r := bot.NewRouter(svc, nil, store, "!xsmb", "!gold", quiet())
	if embed := r.HandleGold(context.Background(), bot.Request{}).Embed; !strings.Contains(embed.Title, "Chưa bật") {
		t.Fatalf("title = %q", embed.Title)
	}
}

// --- chart command ---

func TestGoldChartProducesAnAttachment(t *testing.T) {
	r := newGoldRouter(t, func(context.Context) (domain.GoldBoard, error) { return goldBoard(t), nil })
	reply := r.GoldChart(context.Background(), nil)

	if reply.File == nil {
		t.Fatal("no file attached")
	}
	if reply.File.ContentType != "image/png" {
		t.Fatalf("content type = %q", reply.File.ContentType)
	}
	// The embed must point at the attachment, or Discord shows no picture.
	if reply.Embed.Image == nil ||
		reply.Embed.Image.URL != "attachment://"+reply.File.Name {
		t.Fatalf("embed image = %+v, file = %q", reply.Embed.Image, reply.File.Name)
	}
	raw, err := io.ReadAll(reply.File.Reader)
	if err != nil || len(raw) < 1000 {
		t.Fatalf("attachment is %d bytes, err = %v", len(raw), err)
	}
	if !bytes.HasPrefix(raw, []byte("\x89PNG")) {
		t.Fatal("attachment is not a PNG")
	}
}

func TestGoldChartArgumentsInEitherOrder(t *testing.T) {
	r := newGoldRouter(t, func(context.Context) (domain.GoldBoard, error) { return goldBoard(t), nil })
	for _, args := range [][]string{
		{"sj9999", "7"},
		{"7", "sj9999"},
		{"SJ9999", "7"},
	} {
		reply := r.GoldChart(context.Background(), args)
		if reply.File == nil {
			t.Fatalf("%v: no file", args)
		}
		if reply.File.Name != "gold-sj9999.png" {
			t.Fatalf("%v: file = %q", args, reply.File.Name)
		}
		if !strings.Contains(reply.Embed.Title, "7 ngày") {
			t.Fatalf("%v: title = %q", args, reply.Embed.Title)
		}
	}
}

func TestGoldChartClampsTheDayCount(t *testing.T) {
	r := newGoldRouter(t, func(context.Context) (domain.GoldBoard, error) { return goldBoard(t), nil })
	// The API caps at 30 days.
	if reply := r.GoldChart(context.Background(), []string{"999"}); !strings.Contains(reply.Embed.Title, "30 ngày") {
		t.Fatalf("title = %q", reply.Embed.Title)
	}
	if reply := r.GoldChart(context.Background(), []string{"0"}); reply.File == nil {
		t.Fatal("a zero day count produced no chart")
	}
}

func TestGoldChartDefaultsToTheSJCBar(t *testing.T) {
	r := newGoldRouter(t, func(context.Context) (domain.GoldBoard, error) { return goldBoard(t), nil })
	reply := r.GoldChart(context.Background(), nil)
	if reply.File.Name != "gold-sjl1l10.png" {
		t.Fatalf("file = %q", reply.File.Name)
	}
	if !strings.Contains(reply.Embed.Title, "30 ngày") {
		t.Fatalf("title = %q", reply.Embed.Title)
	}
	if !strings.Contains(reply.Embed.Footer.Text, "mỗi lượng") {
		t.Fatalf("footer = %q", reply.Embed.Footer.Text)
	}
	if !strings.Contains(reply.Embed.Description, "₫") {
		t.Fatalf("chart summary lacks a đồng sign: %q", reply.Embed.Description)
	}
}

func TestGoldChartWithoutConfiguration(t *testing.T) {
	store := memStore()
	svc := service.New(store, &stubProvider{answer: found(t)}, func() time.Time { return at(20, 0) }, quiet())
	r := bot.NewRouter(svc, nil, store, "!xsmb", "!gold", quiet())
	reply := r.GoldChart(context.Background(), nil)
	if reply.File != nil || !strings.Contains(reply.Embed.Title, "Chưa bật") {
		t.Fatalf("embed = %q, file = %v", reply.Embed.Title, reply.File)
	}
}

// futureBoard is a source that has added brands and dropped others.
func futureBoard(t *testing.T) domain.GoldBoard {
	t.Helper()
	quotes := []domain.GoldQuote{
		{Code: "XAUUSD", Name: "Vàng thế giới", Buy: 4565.4, Currency: domain.USD},
		{Code: "SJL1L10", Name: "Vàng miếng SJC", Buy: 143_600_000, Sell: 146_600_000, Currency: domain.VND},
		{Code: "ANCARAT", Name: "Ancarat 9999", Buy: 143_300_000, Sell: 146_800_000, Currency: domain.VND},
		{Code: "MIHONG", Name: "Mi Hong", Buy: 144_200_000, Sell: 146_900_000, Currency: domain.VND},
	}
	board, err := domain.NewGoldBoard(quotes,
		time.Date(2026, 8, 22, 14, 0, 0, 0, domain.Location()), "vang.today",
		time.Date(2026, 8, 22, 14, 5, 0, 0, domain.Location()))
	if err != nil {
		t.Fatal(err)
	}
	return board
}

func helpCodes(t *testing.T, embed *discordgo.MessageEmbed) string {
	t.Helper()
	for _, f := range embed.Fields {
		if strings.Contains(f.Name, "Mã") {
			return f.Value
		}
	}
	t.Fatalf("help has no code list: %+v", embed.Fields)
	return ""
}

// The built-in order and labels must not decide what help advertises.
func TestGoldHelpListsLiveCodesNotTheBuiltInList(t *testing.T) {
	r := newGoldRouter(t, func(context.Context) (domain.GoldBoard, error) { return futureBoard(t), nil })
	codes := helpCodes(t, r.HandleGold(context.Background(), bot.Request{Args: []string{"help"}}).Embed)

	for _, want := range []string{"ANCARAT", "MIHONG"} {
		if !strings.Contains(codes, want) {
			t.Fatalf("help omits the new brand %s: %s", want, codes)
		}
	}
	for _, gone := range []string{"DOHNL", "VIETTINMSJC", "PQHN24NTT"} {
		if strings.Contains(codes, gone) {
			t.Fatalf("help still advertises the retired code %s: %s", gone, codes)
		}
	}
}

// Source down: the built-in list beats an empty answer.
func TestGoldHelpFallsBackWhenTheSourceIsDown(t *testing.T) {
	r := newGoldRouter(t, func(context.Context) (domain.GoldBoard, error) {
		return domain.GoldBoard{}, errors.New("upstream down")
	})
	codes := helpCodes(t, r.HandleGold(context.Background(), bot.Request{Args: []string{"help"}}).Embed)
	if !strings.Contains(codes, "SJL1L10") {
		t.Fatalf("fallback list is empty: %s", codes)
	}
}

func TestGoldHelpWorksWithoutConfiguration(t *testing.T) {
	store := memStore()
	svc := service.New(store, &stubProvider{answer: found(t)}, func() time.Time { return at(20, 0) }, quiet())
	r := bot.NewRouter(svc, nil, store, "!xsmb", "!gold", quiet())
	codes := helpCodes(t, r.HandleGold(context.Background(), bot.Request{Args: []string{"help"}}).Embed)
	if !strings.Contains(codes, "SJL1L10") {
		t.Fatalf("codes = %s", codes)
	}
}

// The built-in list is not a whitelist.
func TestGoldChartAcceptsAnUnknownCode(t *testing.T) {
	r := newGoldRouter(t, func(context.Context) (domain.GoldBoard, error) { return futureBoard(t), nil })
	reply := r.GoldChart(context.Background(), []string{"ancarat", "10"})
	if reply.File == nil {
		t.Fatalf("no chart for an unlisted code: %q", reply.Embed.Title)
	}
	if reply.File.Name != "gold-ancarat.png" {
		t.Fatalf("file = %q", reply.File.Name)
	}
}

// "!gold chart help" used to be read as a gold type named HELP.
func TestGoldChartHelpReachesTheHelpText(t *testing.T) {
	r := newGoldRouter(t, func(context.Context) (domain.GoldBoard, error) { return goldBoard(t), nil })
	for _, args := range [][]string{{"help"}, {"HELP"}, {"sj9999", "help"}} {
		reply := r.GoldChart(context.Background(), args)
		if reply.File != nil {
			t.Fatalf("%v: rendered a chart", args)
		}
		if !strings.Contains(reply.Embed.Title, "Hướng dẫn") {
			t.Fatalf("%v: title = %q", args, reply.Embed.Title)
		}
	}
}

// Help is reachable by one word per prefix, not by a spread of aliases.
func TestHelpHasNoAliases(t *testing.T) {
	r := newGoldRouter(t, func(context.Context) (domain.GoldBoard, error) { return goldBoard(t), nil })
	if title := r.HandleGold(context.Background(), bot.Request{Args: []string{"help"}}).Embed.Title; title != "Hướng dẫn" {
		t.Fatalf("!gold help title = %q", title)
	}
	// "?" is no longer an alias, so it falls through to the price board.
	if title := r.HandleGold(context.Background(), bot.Request{Args: []string{"?"}}).Embed.Title; strings.Contains(title, "Hướng dẫn") {
		t.Fatalf("? still reaches help")
	}
}

// The routing table, exercised without a Discord session.
func TestDispatchRoutesEveryCommand(t *testing.T) {
	r := newGoldRouter(t, func(context.Context) (domain.GoldBoard, error) { return goldBoard(t), nil })
	ctx := context.Background()

	cases := []struct {
		name     string
		kind     bot.Kind
		args     []string
		wantFile bool
		wantIn   string
	}{
		{"lottery latest", bot.KindLottery, nil, false, "XSMB"},
		{"lottery help", bot.KindLottery, []string{"help"}, false, "Hướng dẫn"},
		{"gold board", bot.KindGold, nil, false, "Giá vàng"},
		{"gold help", bot.KindGold, []string{"help"}, false, "Hướng dẫn"},
		{"gold chart", bot.KindGold, []string{"chart"}, true, "30 ngày"},
		{"gold chart args", bot.KindGold, []string{"chart", "dohnl", "7"}, true, "7 ngày"},
		{"gold chart help", bot.KindGold, []string{"chart", "help"}, false, "Hướng dẫn"},
	}
	for _, c := range cases {
		reply := r.Dispatch(ctx, c.kind, bot.Request{Args: c.args})
		if reply.Embed == nil {
			t.Fatalf("%s: no embed", c.name)
		}
		if (reply.File != nil) != c.wantFile {
			t.Fatalf("%s: file = %v, want file = %v", c.name, reply.File != nil, c.wantFile)
		}
		if !strings.Contains(reply.Embed.Title, c.wantIn) {
			t.Fatalf("%s: title = %q, want it to contain %q", c.name, reply.Embed.Title, c.wantIn)
		}
	}
}

// "chart" belongs to the gold prefix only.
func TestDispatchDoesNotLeakChartAcrossPrefixes(t *testing.T) {
	r := newGoldRouter(t, func(context.Context) (domain.GoldBoard, error) { return goldBoard(t), nil })
	reply := r.Dispatch(context.Background(), bot.KindLottery, bot.Request{Args: []string{"chart"}})
	if reply.File != nil {
		t.Fatal("!xsmb chart rendered a gold chart")
	}
	if !strings.Contains(reply.Embed.Title, "Không đọc được ngày") {
		t.Fatalf("title = %q", reply.Embed.Title)
	}
}
