package bot_test

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/minhhdtr/xsmb-discord-bot/internal/bot"
	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// Discord's own rules for a command name, so a bad one is caught here rather
// than by a registration that fails at startup.
var commandName = regexp.MustCompile(`^[-_\p{L}\p{N}]{1,32}$`)

func TestSlashCommandsAreValid(t *testing.T) {
	cmds := bot.SlashCommands(time.Now())
	if len(cmds) == 0 {
		t.Fatal("no commands defined")
	}
	seen := map[string]bool{}
	for _, c := range cmds {
		if !commandName.MatchString(c.Name) || c.Name != strings.ToLower(c.Name) {
			t.Fatalf("bad command name %q", c.Name)
		}
		if seen[c.Name] {
			t.Fatalf("duplicate command %q", c.Name)
		}
		seen[c.Name] = true
		if l := len(c.Description); l == 0 || l > 100 {
			t.Fatalf("%s: description is %d chars", c.Name, l)
		}

		subs, plain := 0, 0
		for _, o := range c.Options {
			if !commandName.MatchString(o.Name) || o.Name != strings.ToLower(o.Name) {
				t.Fatalf("%s: bad option name %q", c.Name, o.Name)
			}
			if l := len(o.Description); l == 0 || l > 100 {
				t.Fatalf("%s.%s: description is %d chars", c.Name, o.Name, l)
			}
			if o.Type == discordgo.ApplicationCommandOptionSubCommand {
				subs++
				for _, so := range o.Options {
					if l := len(so.Description); l == 0 || l > 100 {
						t.Fatalf("%s.%s.%s: description is %d chars", c.Name, o.Name, so.Name, l)
					}
				}
			} else {
				plain++
			}
		}
		// Discord allows either subcommands or plain options, never both.
		if subs > 0 && plain > 0 {
			t.Fatalf("%s mixes %d subcommands with %d options", c.Name, subs, plain)
		}
	}
}

// Required options must come before optional ones, or Discord rejects the
// whole registration.
func TestSlashRequiredOptionsComeFirst(t *testing.T) {
	check := func(path string, opts []*discordgo.ApplicationCommandOption) {
		optionalSeen := false
		for _, o := range opts {
			if o.Required {
				if optionalSeen {
					t.Fatalf("%s: required option %q follows an optional one", path, o.Name)
				}
			} else {
				optionalSeen = true
			}
		}
	}
	for _, c := range bot.SlashCommands(time.Now()) {
		check(c.Name, c.Options)
		for _, o := range c.Options {
			if o.Type == discordgo.ApplicationCommandOptionSubCommand {
				check(c.Name+" "+o.Name, o.Options)
			}
		}
	}
}

func str(name, value string) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{
		Name: name, Type: discordgo.ApplicationCommandOptionString, Value: value}
}

func num(name string, value float64) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{
		Name: name, Type: discordgo.ApplicationCommandOptionInteger, Value: value}
}

func sub(name string, opts ...*discordgo.ApplicationCommandInteractionDataOption) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{
		Name: name, Type: discordgo.ApplicationCommandOptionSubCommand, Options: opts}
}

// Every slash command must land on the same router arguments the prefix form
// produces, so the two ways in cannot drift apart.
func TestSlashRequestMapsOntoTheRouter(t *testing.T) {
	cases := []struct {
		label     string
		data      discordgo.ApplicationCommandInteractionData
		kind      bot.Kind
		args      string
		ephemeral bool
	}{
		{"xsmb bare", discordgo.ApplicationCommandInteractionData{Name: "xsmb"},
			bot.KindLottery, "", false},
		{"xsmb ngay", discordgo.ApplicationCommandInteractionData{Name: "xsmb",
			Options: []*discordgo.ApplicationCommandInteractionDataOption{str("ngay", "14/08/2026")}},
			bot.KindLottery, "14/08/2026", false},
		{"gold", discordgo.ApplicationCommandInteractionData{Name: "gold"},
			bot.KindGold, "", false},
		{"bieudo bare", discordgo.ApplicationCommandInteractionData{Name: "bieudo"},
			bot.KindGold, "chart", false},
		{"bieudo full", discordgo.ApplicationCommandInteractionData{Name: "bieudo",
			Options: []*discordgo.ApplicationCommandInteractionDataOption{str("ma", "DOHNL"), num("ngay", 7)}},
			bot.KindGold, "chart DOHNL 7", false},
		{"thongke logan", discordgo.ApplicationCommandInteractionData{Name: "thongke",
			Options: []*discordgo.ApplicationCommandInteractionDataOption{sub("logan")}},
			bot.KindLottery, "logan", false},
		{"thongke tanso", discordgo.ApplicationCommandInteractionData{Name: "thongke",
			Options: []*discordgo.ApplicationCommandInteractionDataOption{sub("tanso", num("ngay", 90))}},
			bot.KindLottery, "tanso 90", false},
		{"thongke lo", discordgo.ApplicationCommandInteractionData{Name: "thongke",
			Options: []*discordgo.ApplicationCommandInteractionDataOption{sub("lo", str("so", "88"))}},
			bot.KindLottery, "lo 88", false},
		{"thongke db", discordgo.ApplicationCommandInteractionData{Name: "thongke",
			Options: []*discordgo.ApplicationCommandInteractionDataOption{sub("db", str("thang", "08/2026"))}},
			bot.KindLottery, "db 08/2026", false},
		{"thongke kho", discordgo.ApplicationCommandInteractionData{Name: "thongke",
			Options: []*discordgo.ApplicationCommandInteractionDataOption{sub("kho")}},
			bot.KindLottery, "status", false},
		{"thongbao bat", discordgo.ApplicationCommandInteractionData{Name: "thongbao",
			Options: []*discordgo.ApplicationCommandInteractionDataOption{str("trangthai", "sub")}},
			bot.KindLottery, "sub", true},
		{"huongdan", discordgo.ApplicationCommandInteractionData{Name: "huongdan"},
			bot.KindLottery, "help", true},
		{"lệnh lạ", discordgo.ApplicationCommandInteractionData{Name: "khongcolenhnay"},
			bot.KindNone, "", false},
	}
	for _, c := range cases {
		kind, args, eph := bot.SlashRequest(c.data)
		if kind != c.kind {
			t.Fatalf("%s: kind = %v, want %v", c.label, kind, c.kind)
		}
		if got := strings.Join(args, " "); got != c.args {
			t.Fatalf("%s: args = %q, want %q", c.label, got, c.args)
		}
		if eph != c.ephemeral {
			t.Fatalf("%s: ephemeral = %v, want %v", c.label, eph, c.ephemeral)
		}
	}
}

// The point of one router: a slash command and its prefix twin must produce
// the same answer.
func TestSlashAndPrefixAgree(t *testing.T) {
	r := newGoldRouter(t, func(context.Context) (domain.GoldBoard, error) { return goldBoard(t), nil })
	ctx := context.Background()

	pairs := []struct {
		slash  discordgo.ApplicationCommandInteractionData
		prefix string
	}{
		{discordgo.ApplicationCommandInteractionData{Name: "xsmb"}, "!xsmb"},
		{discordgo.ApplicationCommandInteractionData{Name: "gold"}, "!gold"},
		{discordgo.ApplicationCommandInteractionData{Name: "huongdan"}, "!xsmb help"},
		{discordgo.ApplicationCommandInteractionData{Name: "thongke",
			Options: []*discordgo.ApplicationCommandInteractionDataOption{sub("lo", str("so", "88"))}}, "!xsmb lo 88"},
		{discordgo.ApplicationCommandInteractionData{Name: "bieudo",
			Options: []*discordgo.ApplicationCommandInteractionDataOption{str("ma", "sj9999")}}, "!gold chart sj9999"},
	}
	for _, p := range pairs {
		sk, sa, _ := bot.SlashRequest(p.slash)
		pk, pa := r.Match(p.prefix)
		if sk != pk {
			t.Fatalf("%s: slash kind %v, prefix kind %v", p.prefix, sk, pk)
		}
		fromSlash := r.Dispatch(ctx, sk, bot.Request{Args: sa}).Embed
		fromPrefix := r.Dispatch(ctx, pk, bot.Request{Args: pa}).Embed
		if fromSlash.Title != fromPrefix.Title {
			t.Fatalf("%s: slash %q vs prefix %q", p.prefix, fromSlash.Title, fromPrefix.Title)
		}
	}
}

// --- autocomplete ---

func TestGoldChoicesComeFromTheLiveBoard(t *testing.T) {
	r := newGoldRouter(t, func(context.Context) (domain.GoldBoard, error) { return futureBoard(t), nil })
	choices := r.GoldChoices(context.Background(), "")

	if len(choices) != 4 {
		t.Fatalf("got %d choices, want one per live quote", len(choices))
	}
	var values []string
	for _, c := range choices {
		values = append(values, c.Value.(string))
	}
	joined := strings.Join(values, ",")
	for _, want := range []string{"ANCARAT", "MIHONG"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("new brand %s missing: %v", want, values)
		}
	}
	for _, gone := range []string{"DOHNL", "PQHNVM"} {
		if strings.Contains(joined, gone) {
			t.Fatalf("retired code %s still offered: %v", gone, values)
		}
	}
	// The label a person reads should be the name, with the code alongside.
	if !strings.Contains(choices[0].Name, "Vàng thế giới") || !strings.Contains(choices[0].Name, "XAUUSD") {
		t.Fatalf("choice label = %q", choices[0].Name)
	}
}

func TestGoldChoicesFilterOnWhatIsTyped(t *testing.T) {
	r := newGoldRouter(t, func(context.Context) (domain.GoldBoard, error) { return goldBoard(t), nil })
	ctx := context.Background()

	for _, c := range []struct{ typed, want string }{
		{"sj", "SJL1L10"},
		{"SJ", "SJL1L10"},
		{"doji", "DOJINHTV"},
		{"nữ trang", "DOJINHTV"},
	} {
		got := r.GoldChoices(ctx, c.typed)
		if len(got) == 0 {
			t.Fatalf("typing %q matched nothing", c.typed)
		}
		found := false
		for _, ch := range got {
			if ch.Value.(string) == c.want {
				found = true
			}
		}
		if !found {
			t.Fatalf("typing %q did not offer %s", c.typed, c.want)
		}
	}
	if got := r.GoldChoices(ctx, "khongcomanay"); len(got) != 0 {
		t.Fatalf("nonsense matched %d choices", len(got))
	}
}

func TestGoldChoicesFallBackWhenTheSourceIsDown(t *testing.T) {
	r := newGoldRouter(t, func(context.Context) (domain.GoldBoard, error) {
		return domain.GoldBoard{}, errors.New("upstream down")
	})
	choices := r.GoldChoices(context.Background(), "")
	if len(choices) == 0 {
		t.Fatal("no fallback choices")
	}
	if len(choices) > 25 {
		t.Fatalf("%d choices, over Discord's limit of 25", len(choices))
	}
}

// Discord rejects a reply with more than 25 suggestions.
func TestGoldChoicesStayUnderDiscordsLimit(t *testing.T) {
	quotes := make([]domain.GoldQuote, 0, 40)
	for i := 0; i < 40; i++ {
		quotes = append(quotes, domain.GoldQuote{
			Code: fmt.Sprintf("CODE%02d", i), Name: fmt.Sprintf("Thương hiệu %d", i),
			Buy: 143_000_000, Sell: 146_000_000, Currency: domain.VND,
		})
	}
	board, err := domain.NewGoldBoard(quotes, at(14, 0), "test", at(14, 0))
	if err != nil {
		t.Fatal(err)
	}
	r := newGoldRouter(t, func(context.Context) (domain.GoldBoard, error) { return board, nil })
	if got := len(r.GoldChoices(context.Background(), "")); got != 25 {
		t.Fatalf("got %d choices, want the cap of 25", got)
	}
}

// --- help sections ---

func TestHelpListsEverythingByDefault(t *testing.T) {
	r := newGoldRouter(t, func(context.Context) (domain.GoldBoard, error) { return goldBoard(t), nil })
	embed := r.Handle(context.Background(), bot.Request{Args: []string{"help"}}).Embed

	var body strings.Builder
	for _, f := range embed.Fields {
		body.WriteString(f.Name + " " + f.Value + "\n")
	}
	text := body.String()
	for _, want := range []string{"/xsmb", "/thongke logan", "/thongke lo", "/gold", "/bieudo", "/thongbao"} {
		if !strings.Contains(text, want) {
			t.Fatalf("%s missing from the full help:\n%s", want, text)
		}
	}
	if !strings.Contains(embed.Footer.Text, "!xsmb") {
		t.Fatalf("footer does not mention the prefix form: %q", embed.Footer.Text)
	}
}

func TestHelpSections(t *testing.T) {
	r := newGoldRouter(t, func(context.Context) (domain.GoldBoard, error) { return goldBoard(t), nil })
	ctx := context.Background()

	lottery := r.Handle(ctx, bot.Request{Args: []string{"help", "xsmb"}}).Embed
	var lot strings.Builder
	for _, f := range lottery.Fields {
		lot.WriteString(f.Name + " " + f.Value + "\n")
	}
	if strings.Contains(lot.String(), "/gold") {
		t.Fatalf("the xsmb section includes gold commands:\n%s", lot.String())
	}
	if !strings.Contains(lot.String(), "/thongke logan") {
		t.Fatal("the xsmb section is missing statistics")
	}

	gold := r.Handle(ctx, bot.Request{Args: []string{"help", "vang"}}).Embed
	var g strings.Builder
	for _, f := range gold.Fields {
		g.WriteString(f.Name + " " + f.Value + "\n")
	}
	if strings.Contains(g.String(), "/thongke logan") {
		t.Fatalf("the gold section includes lottery commands:\n%s", g.String())
	}
	if !strings.Contains(g.String(), "Mã đang có") {
		t.Fatal("the gold section is missing the live code list")
	}
}

func TestHelpFitsDiscordLimits(t *testing.T) {
	r := newGoldRouter(t, func(context.Context) (domain.GoldBoard, error) { return goldBoard(t), nil })
	for _, args := range [][]string{{"help"}, {"help", "xsmb"}, {"help", "vang"}} {
		embed := r.Handle(context.Background(), bot.Request{Args: args}).Embed
		total := len(embed.Title)
		if embed.Footer != nil {
			total += len(embed.Footer.Text)
		}
		for _, f := range embed.Fields {
			if len(f.Value) > 1024 {
				t.Fatalf("%v: field %q is %d chars", args, f.Name, len(f.Value))
			}
			total += len(f.Name) + len(f.Value)
		}
		if len(embed.Fields) > 25 || total > 6000 {
			t.Fatalf("%v: %d fields, %d chars", args, len(embed.Fields), total)
		}
	}
}

// /huongdan phan:… must reach the same sections the prefix form does.
func TestSlashHelpSectionMapping(t *testing.T) {
	for _, c := range []struct{ value, want string }{{"", "help"}, {"xsmb", "help xsmb"}, {"vang", "help vang"}} {
		data := discordgo.ApplicationCommandInteractionData{Name: "huongdan"}
		if c.value != "" {
			data.Options = []*discordgo.ApplicationCommandInteractionDataOption{str("phan", c.value)}
		}
		kind, args, eph := bot.SlashRequest(data)
		if kind != bot.KindLottery || !eph {
			t.Fatalf("%q: kind=%v ephemeral=%v", c.value, kind, eph)
		}
		if got := strings.Join(args, " "); got != c.want {
			t.Fatalf("%q: args = %q, want %q", c.value, got, c.want)
		}
	}
}

// The example dates come from the clock, so they must survive the awkward
// days: a month start, a 31st rolling back into a 30-day month, and either
// side of the 18:35 cutoff.
func TestSlashExampleDatesFollowTheClock(t *testing.T) {
	dayOption := func(cmds []*discordgo.ApplicationCommand, name string) string {
		t.Helper()
		for _, c := range cmds {
			if c.Name == name {
				return c.Options[0].Description
			}
		}
		t.Fatalf("no command %q", name)
		return ""
	}
	monthOption := func(cmds []*discordgo.ApplicationCommand) string {
		t.Helper()
		for _, c := range cmds {
			if c.Name != "thongke" {
				continue
			}
			for _, sub := range c.Options {
				if sub.Name == "db" {
					return sub.Options[0].Description
				}
			}
		}
		t.Fatal("no /thongke db")
		return ""
	}

	cases := []struct {
		name      string
		now       time.Time
		wantDay   string
		wantMonth string
	}{
		{
			// Before 18:35 the newest result is yesterday's.
			name:    "trước giờ quay",
			now:     time.Date(2026, 9, 4, 8, 0, 0, 0, domain.Location()),
			wantDay: "03/09/2026", wantMonth: "08/2026",
		},
		{
			// After it, today's.
			name:    "sau giờ quay",
			now:     time.Date(2026, 9, 4, 19, 0, 0, 0, domain.Location()),
			wantDay: "04/09/2026", wantMonth: "08/2026",
		},
		{
			// The 1st: the previous month is the one before, not this one.
			name:    "ngày mùng 1",
			now:     time.Date(2026, 9, 1, 19, 0, 0, 0, domain.Location()),
			wantDay: "01/09/2026", wantMonth: "08/2026",
		},
		{
			// A 31st rolling back into a 30-day month. AddDate(0, -1, 0) here
			// would give 01/07 instead of July.
			name:    "ngày 31",
			now:     time.Date(2026, 7, 31, 19, 0, 0, 0, domain.Location()),
			wantDay: "31/07/2026", wantMonth: "06/2026",
		},
		{
			// Across a year boundary.
			name:    "tháng 1",
			now:     time.Date(2027, 1, 5, 19, 0, 0, 0, domain.Location()),
			wantDay: "05/01/2027", wantMonth: "12/2026",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmds := bot.SlashCommands(tc.now)
			if got := dayOption(cmds, "xsmb"); !strings.Contains(got, tc.wantDay) {
				t.Errorf("/xsmb ngay = %q, want it to mention %s", got, tc.wantDay)
			}
			if got := monthOption(cmds); !strings.Contains(got, tc.wantMonth) {
				t.Errorf("/thongke db thang = %q, want it to mention %s", got, tc.wantMonth)
			}
		})
	}
}

// Discord rejects an option description over 100 characters, and a dynamic
// date makes the length easy to lose track of.
func TestSlashDescriptionsFitDiscordsLimit(t *testing.T) {
	var walk func(path string, opts []*discordgo.ApplicationCommandOption)
	walk = func(path string, opts []*discordgo.ApplicationCommandOption) {
		for _, o := range opts {
			if n := len([]rune(o.Description)); n > 100 {
				t.Errorf("%s %s: description is %d runes, max 100", path, o.Name, n)
			}
			walk(path+" "+o.Name, o.Options)
		}
	}
	for _, c := range bot.SlashCommands(time.Now()) {
		if n := len([]rune(c.Description)); n > 100 {
			t.Errorf("%s: description is %d runes, max 100", c.Name, n)
		}
		walk(c.Name, c.Options)
	}
}
