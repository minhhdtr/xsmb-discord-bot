package bot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/minhhdtr/xsmb-discord-bot/internal/chart"
	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/format"
	"github.com/minhhdtr/xsmb-discord-bot/internal/provider"
)

// Request is one command without Discord's plumbing, so the router is
// testable without a session.
type Request struct {
	Args      []string
	GuildID   string
	ChannelID string
	CanManage bool // may the caller change this server's subscription
}

// Router turns a Request into the embed to reply with.
type Router struct {
	core       Core
	now        func() time.Time
	prefix     string
	goldPrefix string
	log        *slog.Logger

	// spins reserves a channel while a quay thử plays out there, so two spins
	// cannot interleave their edits and burn the channel's edit budget.
	spinMu sync.Mutex
	spins  map[string]time.Time
}

// NewRouter builds a command router. gold may be nil.
func NewRouter(core Core, clock func() time.Time, prefix, goldPrefix string, log *slog.Logger) *Router {
	if clock == nil {
		clock = func() time.Time { return time.Now().In(domain.Location()) }
	}
	if log == nil {
		log = slog.Default()
	}
	return &Router{core: core, now: clock,
		prefix: prefix, goldPrefix: goldPrefix, log: log,
		spins: make(map[string]time.Time)}
}

// Kind says which command a message invoked.
type Kind int

const (
	// KindNone means the message was not for us.
	KindNone Kind = iota
	// KindLottery is the lottery prefix.
	KindLottery
	// KindGold is the gold prefix.
	KindGold
)

// Match works out which command a message invokes, if any.
func (r *Router) Match(content string) (Kind, []string) {
	if args, ok := ParseCommand(content, r.prefix); ok {
		return KindLottery, args
	}
	if r.goldPrefix != "" {
		if args, ok := ParseCommand(content, r.goldPrefix); ok {
			return KindGold, args
		}
	}
	return KindNone, nil
}

// Reply is what a command returns. Only the chart uses File so far, but
// having one type means adding an attachment elsewhere is a local change.
type Reply struct {
	Embed *discordgo.MessageEmbed
	File  *discordgo.File

	// Frames are later states of the same message, shown one after another by
	// editing it. Embed is the first. The router builds them all up front and
	// the gateway handler does the waiting, so nothing here needs a session.
	Frames []*discordgo.MessageEmbed
	Pace   time.Duration
}

// embed wraps a reply with nothing attached. The helpers below build embeds
// directly; only the entry points deal in Reply.
func embed(e *discordgo.MessageEmbed) Reply { return Reply{Embed: e} }

// Dispatch picks the command for a matched message. Keeping the routing
// table here means adding a subcommand doesn't touch the gateway handler.
func (r *Router) Dispatch(ctx context.Context, kind Kind, req Request) Reply {
	if kind == KindGold {
		if len(req.Args) > 0 && strings.EqualFold(req.Args[0], "chart") {
			return r.GoldChart(ctx, req.Args[1:])
		}
		return r.HandleGold(ctx, req)
	}
	return r.Handle(ctx, req)
}

// GoldChart renders a price history for one gold type. The chart font is
// ASCII only, so anything with diacritics goes in the embed instead.
func (r *Router) GoldChart(ctx context.Context, args []string) Reply {
	// "!gold chart help" must reach the help text. Without this it becomes a
	// request for a gold type named HELP, which fails upstream and reports a
	// missing price rather than showing the usage.
	for _, arg := range args {
		if strings.EqualFold(arg, "help") {
			return embed(r.goldHelp(ctx))
		}
	}
	code, days := parseChartArgs(args)

	series, err := r.core.GoldHistory(ctx, code, days)
	if errors.Is(err, ErrNotConfigured) {
		return embed(NoticeEmbed("Chưa bật", "Tính năng giá vàng chưa được cấu hình.", true))
	}
	if err != nil {
		r.log.Error("gold history failed", "code", code, "days", days, "error", err)
		return embed(NoticeEmbed("Lỗi",
			fmt.Sprintf("Không lấy được lịch sử giá của `%s`. Thử lại sau nhé.", code), true))
	}
	if series.Len() < 2 {
		return embed(NoticeEmbed("Chưa đủ dữ liệu",
			fmt.Sprintf("Chỉ có %d ngày cho `%s`, không vẽ được đường.", series.Len(), code), false))
	}

	png, err := chart.Render(series)
	if err != nil {
		r.log.Error("cannot render chart", "code", code, "error", err)
		return embed(NoticeEmbed("Lỗi", "Không vẽ được biểu đồ.", true))
	}

	unit, render := "mỗi lượng", format.Dong
	if series.Currency == domain.USD {
		unit, render = "USD/oz", func(v float64) string { return format.Decimal(v, 1) }
	}
	first, last := series.First(), series.Last()

	// Not named "embed": that would shadow the helper of the same name for the
	// rest of this function.
	card := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("📈 %s · %d ngày", series.Name, series.Len()),
		Color: colourGold,
		Description: fmt.Sprintf("%s → %s  ·  %s (%s)",
			render(first.Buy), render(last.Buy),
			changeIn(series), signedPercent(series.ChangePercent())),
		Image:  &discordgo.MessageEmbedImage{URL: "attachment://" + chartFileName(code)},
		Footer: &discordgo.MessageEmbedFooter{Text: "Nguồn vang.today · " + unit},
	}
	return Reply{
		Embed: card,
		File: &discordgo.File{
			Name:        chartFileName(code),
			ContentType: "image/png",
			Reader:      bytes.NewReader(png),
		},
	}
}

// parseChartArgs reads an optional gold code and day count, in either order,
// so "!gold chart 7 sj9999" works as well as "!gold chart sj9999 7".
func parseChartArgs(args []string) (code string, days int) {
	code, days = "SJL1L10", domain.MaxHistoryDays
	for _, arg := range args {
		if n, err := strconv.Atoi(arg); err == nil {
			if n < 2 {
				n = 2
			}
			if n > domain.MaxHistoryDays {
				n = domain.MaxHistoryDays
			}
			days = n
			continue
		}
		if upper := strings.ToUpper(arg); upper != "" {
			code = upper
		}
	}
	return code, days
}

func chartFileName(code string) string {
	return "gold-" + strings.ToLower(code) + ".png"
}

// changeIn renders the movement over the whole series in the same unit as
// the prices beside it.
func changeIn(series domain.GoldSeries) string {
	if series.Currency == domain.USD {
		return changeLabel(series.Change(), 1)
	}
	return changeDong(series.Change())
}

func signedPercent(v float64) string {
	sign := "+"
	if v < 0 {
		sign = "-"
		v = -v
	}
	return sign + format.Decimal(v, 2) + "%"
}

// goldHelp lists the codes the source is serving right now rather than the
// built-in list, so a new brand shows up and a dropped one stops being
// advertised.
func (r *Router) goldHelp(ctx context.Context) *discordgo.MessageEmbed {
	codes := r.liveGoldCodes(ctx)
	if len(codes) == 0 {
		codes = provider.GoldCodes()
	}
	return &discordgo.MessageEmbed{
		Title: "Hướng dẫn",
		Color: colourGold,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "/gold", Value: "Giá vàng trong nước và thế giới, cập nhật vài phút một lần."},
			{Name: "/bieudo", Value: "Biểu đồ 30 ngày của vàng miếng SJC."},
			{Name: "/bieudo ma:dohnl ngay:7", Value: "Biểu đồ 7 ngày của một mã khác."},
			{Name: "Mã đang có", Value: "`" + strings.Join(codes, "`, `") + "`"},
			{Name: "Prefix", Value: "`" + r.goldPrefix + "`, `" + r.goldPrefix + " chart dohnl 7`"},
		},
	}
}

// maxChoices is Discord's ceiling on autocomplete suggestions.
const maxChoices = 25

// GoldChoices suggests gold codes matching what the user has typed so far,
// taken from the live board with the built-in list as fallback.
func (r *Router) GoldChoices(ctx context.Context, typed string) []*discordgo.ApplicationCommandOptionChoice {
	type entry struct{ code, label string }
	var entries []entry

	// Whether gold is configured at all is core's business now, and it says so
	// with an error like any other. Falling back to the built-in list covers
	// both "not configured" and "source unreachable", which want the same
	// answer here anyway.
	if board, err := r.core.GoldBoard(ctx); err == nil {
		for _, q := range board.Quotes() {
			entries = append(entries, entry{q.Code, q.Name})
		}
	} else {
		r.log.Warn("autocomplete fell back to the built-in code list", "error", err)
	}
	if len(entries) == 0 {
		for _, code := range provider.GoldCodes() {
			entries = append(entries, entry{code, code})
		}
	}

	needle := strings.ToLower(strings.TrimSpace(typed))
	out := make([]*discordgo.ApplicationCommandOptionChoice, 0, maxChoices)
	for _, e := range entries {
		if len(out) >= maxChoices {
			break
		}
		if needle != "" &&
			!strings.Contains(strings.ToLower(e.code), needle) &&
			!strings.Contains(strings.ToLower(e.label), needle) {
			continue
		}
		name := e.label
		if !strings.EqualFold(e.label, e.code) {
			name = e.label + " (" + e.code + ")"
		}
		if len(name) > 100 {
			name = name[:100]
		}
		out = append(out, &discordgo.ApplicationCommandOptionChoice{Name: name, Value: e.code})
	}
	return out
}

// liveGoldCodes reads the codes the source is serving now. Empty when the
// source cannot be reached, so callers can fall back.
func (r *Router) liveGoldCodes(ctx context.Context) []string {
	board, err := r.core.GoldBoard(ctx)
	if err != nil {
		r.log.Warn("gold help fell back to the built-in code list", "error", err)
		return nil
	}
	codes := make([]string, 0, len(board.Quotes()))
	for _, q := range board.Quotes() {
		codes = append(codes, q.Code)
	}
	return codes
}

// HandleGold answers the gold command.
func (r *Router) HandleGold(ctx context.Context, req Request) Reply {
	if len(req.Args) > 0 && strings.EqualFold(req.Args[0], "help") {
		return embed(r.goldHelp(ctx))
	}
	board, err := r.core.GoldBoard(ctx)
	if errors.Is(err, ErrNotConfigured) {
		return embed(NoticeEmbed("Chưa bật", "Tính năng giá vàng chưa được cấu hình.", true))
	}
	if err != nil {
		r.log.Error("gold command failed", "error", err)
		return embed(NoticeEmbed("Lỗi", "Không lấy được giá vàng lúc này. Thử lại sau nhé.", true))
	}
	return embed(GoldEmbed(board))
}

// ParseCommand splits a message into arguments if it invokes this bot.
// Case-insensitive, tolerant of the extra spaces phone keyboards insert.
func ParseCommand(content, prefix string) ([]string, bool) {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(strings.ToLower(trimmed), strings.ToLower(prefix)) {
		return nil, false
	}
	rest := strings.TrimSpace(trimmed[len(prefix):])
	if rest == "" {
		return []string{}, true
	}
	// Reject "!xsmbfoo": the prefix must end on a word boundary.
	if !isSpaceByte(trimmed[len(prefix)]) {
		return nil, false
	}
	return strings.Fields(rest), true
}

func isSpaceByte(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

// Handle answers the lottery command.
func (r *Router) Handle(ctx context.Context, req Request) Reply {
	if len(req.Args) == 0 {
		return embed(r.latest(ctx))
	}
	switch strings.ToLower(req.Args[0]) {
	case "help":
		return embed(r.help(ctx, req.Args[1:]))
	case "sub", "dangky", "on":
		return embed(r.subscribe(ctx, req))
	case "unsub", "huy", "off":
		return embed(r.unsubscribe(ctx, req))
	case "status":
		// "stats" and "thongke" used to alias this; they now read as the statistics
		// commands below.
		return embed(r.status(ctx))
	case "logan":
		return embed(r.loGan(ctx))
	case "degan":
		return embed(r.deGan(ctx))
	case "tanso":
		return embed(r.tanSo(ctx, req.Args[1:]))
	case "lo":
		return embed(r.loProfile(ctx, req.Args[1:]))
	case "db":
		return embed(r.specialMonth(ctx, req.Args[1:]))
	case "ngay":
		return embed(r.dayReport(ctx, req.Args[1:]))
	case "quaythu", "quaythử":
		return r.quayThu(ctx, req.ChannelID)
	default:
		return embed(r.byDate(ctx, strings.Join(req.Args, " ")))
	}
}

// ganLimit is how many numbers a drought ranking shows.
const ganLimit = 12

func (r *Router) loGan(ctx context.Context) *discordgo.MessageEmbed {
	entries, err := r.core.LoGan(ctx, ganLimit)
	if err != nil {
		r.log.Error("lo gan failed", "error", err)
		return NoticeEmbed("Lỗi", "Không đọc được thống kê. Thử lại sau nhé.", true)
	}
	size, asOf := r.archive(ctx)
	return GanEmbed("🔴 Lô gan", entries, size, asOf)
}

func (r *Router) deGan(ctx context.Context) *discordgo.MessageEmbed {
	entries, err := r.core.DeGan(ctx, ganLimit)
	if err != nil {
		r.log.Error("de gan failed", "error", err)
		return NoticeEmbed("Lỗi", "Không đọc được thống kê. Thử lại sau nhé.", true)
	}
	size, asOf := r.archive(ctx)
	return GanEmbed("🎱 Đề gan", entries, size, asOf)
}

// defaultWindow is the frequency window when none is given.
const defaultWindow = 30

func (r *Router) tanSo(ctx context.Context, args []string) *discordgo.MessageEmbed {
	days := defaultWindow
	grouping, grouped := domain.ByHead, false

	// Either argument can come first: a number is a window, a word is a
	// grouping. Insisting on an order would only make the command harder to
	// remember.
	for _, arg := range args {
		if n, err := strconv.Atoi(arg); err == nil {
			if n < 1 {
				return NoticeEmbed("Không đọc được số ngày",
					fmt.Sprintf("%q phải là số ngày dương.\nDùng dạng `%s tanso 90`.",
						arg, r.prefix), false)
			}
			days = n
			continue
		}
		g, err := domain.ParseGrouping(arg)
		if err != nil {
			// The argument is neither form, so naming only one of them would
			// send the reader looking in the wrong place.
			return NoticeEmbed("Không đọc được tham số",
				fmt.Sprintf("Mình không hiểu %q.\nTham số là số ngày, hoặc kiểu gom: "+
					"`dau`, `duoi`, `tong`, `cham`.\nDùng dạng `%s tanso 90 dau`.",
					arg, r.prefix), false)
		}
		grouping, grouped = g, true
	}

	freq, err := r.core.Frequency(ctx, days)
	if err != nil {
		r.log.Error("frequency failed", "days", days, "error", err)
		return NoticeEmbed("Lỗi", "Không đọc được thống kê. Thử lại sau nhé.", true)
	}
	size, _ := r.archive(ctx)
	if grouped {
		return GroupedFrequencyEmbed(domain.GroupFrequency(freq, grouping), days, size)
	}
	return FrequencyEmbed(freq, days, size)
}

func (r *Router) loProfile(ctx context.Context, args []string) *discordgo.MessageEmbed {
	if len(args) == 0 {
		return NoticeEmbed("Thiếu số",
			fmt.Sprintf("Dùng dạng `%s lo 88`.", r.prefix), false)
	}
	lo, err := domain.ParseLo(args[0])
	if err != nil {
		return NoticeEmbed("Không đọc được số",
			fmt.Sprintf("%q không phải số hai chữ số.\nDùng dạng `%s lo 88`.", args[0], r.prefix), false)
	}
	profile, err := r.core.Profile(ctx, lo)
	if err != nil {
		r.log.Error("profile failed", "lo", lo, "error", err)
		return NoticeEmbed("Lỗi", "Không đọc được thống kê. Thử lại sau nhé.", true)
	}
	return ProfileEmbed(profile)
}

func (r *Router) specialMonth(ctx context.Context, args []string) *discordgo.MessageEmbed {
	now := r.now()
	year, month := now.Year(), now.Month()
	if len(args) > 0 {
		parsedYear, parsedMonth, err := domain.ParseMonth(strings.Join(args, " "), now)
		if err != nil {
			return NoticeEmbed("Không đọc được tháng",
				fmt.Sprintf("Mình không hiểu %q.\nDùng dạng `%s db 08/2026`.",
					strings.Join(args, " "), r.prefix), false)
		}
		year, month = parsedYear, parsedMonth
	}
	days, err := r.core.SpecialMonth(ctx, year, month)
	if err != nil {
		r.log.Error("special month failed", "year", year, "month", month, "error", err)
		return NoticeEmbed("Lỗi", "Không đọc được thống kê. Thử lại sau nhé.", true)
	}
	return SpecialMonthEmbed(days, year, int(month))
}

// dayReport analyses one draw. No argument means the newest one, so the
// common case is a bare command.
func (r *Router) dayReport(ctx context.Context, args []string) *discordgo.MessageEmbed {
	var (
		draw domain.Draw
		err  error
	)
	if len(args) == 0 {
		draw, err = r.core.Latest(ctx)
	} else {
		raw := strings.Join(args, " ")
		day, parseErr := domain.ParseDate(raw, r.now())
		if parseErr != nil {
			return NoticeEmbed("Không đọc được ngày",
				fmt.Sprintf("Mình không hiểu %q.\nDùng dạng `%s ngay 03/09/2026`.",
					raw, r.prefix), false)
		}
		draw, err = r.core.Draw(ctx, day)
	}
	if err != nil {
		return r.explain(err)
	}
	return DayReportEmbed(draw)
}

// archive returns the draw count and newest day for the footers. One call,
// not two - the summary costs three queries.
func (r *Router) archive(ctx context.Context) (size int, asOf string) {
	stats, err := r.core.Archive(ctx)
	if err != nil {
		return 0, domain.FormatVN(r.now())
	}
	asOf = domain.FormatVN(r.now())
	if !stats.Latest.IsZero() {
		asOf = domain.FormatVN(stats.Latest)
	}
	return stats.Draws, asOf
}

func (r *Router) latest(ctx context.Context) *discordgo.MessageEmbed {
	draw, err := r.core.Latest(ctx)
	if err != nil {
		return r.explain(err)
	}
	return DrawEmbed(draw, false)
}

func (r *Router) byDate(ctx context.Context, raw string) *discordgo.MessageEmbed {
	day, err := domain.ParseDate(raw, r.now())
	if err != nil {
		return NoticeEmbed("Không đọc được ngày",
			fmt.Sprintf("Mình không hiểu %q.\nDùng dạng `%s 14/08/2026`.", raw, r.prefix), false)
	}
	draw, err := r.core.Draw(ctx, day)
	if err != nil {
		return r.explain(err)
	}
	return DrawEmbed(draw, false)
}

func (r *Router) subscribe(ctx context.Context, req Request) *discordgo.MessageEmbed {
	if req.GuildID == "" {
		return NoticeEmbed("Chỉ dùng trong server",
			"Lệnh này cần chạy trong một kênh của server, không dùng được ở tin nhắn riêng.", true)
	}
	if !req.CanManage {
		return NoticeEmbed("Không đủ quyền",
			"Cần quyền **Quản lý kênh** để bật thông báo tự động.", true)
	}
	added, err := r.core.Subscribe(ctx, req.GuildID, req.ChannelID)
	if err != nil {
		r.log.Error("subscribe failed", "channel", req.ChannelID, "error", err)
		return NoticeEmbed("Lỗi", "Không lưu được đăng ký. Thử lại sau nhé.", true)
	}
	if !added {
		return NoticeEmbed("Đã bật từ trước", "Kênh này vốn đã nhận thông báo lúc 18h35 rồi.", false)
	}
	return NoticeEmbed("Đã bật thông báo",
		"Từ giờ kênh này sẽ nhận kết quả XSMB tự động lúc **18h35** hằng ngày.", false)
}

func (r *Router) unsubscribe(ctx context.Context, req Request) *discordgo.MessageEmbed {
	if !req.CanManage {
		return NoticeEmbed("Không đủ quyền",
			"Cần quyền **Quản lý kênh** để tắt thông báo tự động.", true)
	}
	removed, err := r.core.Unsubscribe(ctx, req.ChannelID)
	if err != nil {
		r.log.Error("unsubscribe failed", "channel", req.ChannelID, "error", err)
		return NoticeEmbed("Lỗi", "Không huỷ được đăng ký. Thử lại sau nhé.", true)
	}
	if !removed {
		return NoticeEmbed("Chưa bật", "Kênh này vốn không nhận thông báo tự động.", false)
	}
	return NoticeEmbed("Đã tắt thông báo", "Kênh này sẽ không nhận kết quả tự động nữa.", false)
}

func (r *Router) status(ctx context.Context) *discordgo.MessageEmbed {
	stats, err := r.core.Archive(ctx)
	if err != nil {
		r.log.Error("stats failed", "error", err)
		return NoticeEmbed("Lỗi", "Không đọc được kho dữ liệu.", true)
	}
	body := fmt.Sprintf("Đang lưu **%d** kỳ quay.", stats.Draws)
	if stats.Draws > 0 {
		body += fmt.Sprintf("\nTừ %s đến %s.", domain.FormatVN(stats.Earliest), domain.FormatVN(stats.Latest))
	}
	body += fmt.Sprintf("\n**%d** kênh đang bật thông báo tự động.", stats.Channels)
	return NoticeEmbed("Tình trạng", body, false)
}

// help lists commands. With no section it lists everything, which is what a
// bare /huongdan should do; a section narrows it.
func (r *Router) help(ctx context.Context, args []string) *discordgo.MessageEmbed {
	section := ""
	if len(args) > 0 {
		section = strings.ToLower(args[0])
	}
	if section == "vang" || section == "gold" {
		return r.goldHelp(ctx)
	}

	embed := &discordgo.MessageEmbed{
		Title:  "Hướng dẫn",
		Color:  colourQuery,
		Fields: []*discordgo.MessageEmbedField{{Name: "Kết quả", Value: r.resultHelp()}, {Name: "Thống kê", Value: r.statsHelp()}},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Dạng %s và %s cũ vẫn dùng được", r.prefix, r.goldPrefix),
		},
	}
	if section == "xsmb" {
		return embed
	}
	embed.Fields = append(embed.Fields,
		&discordgo.MessageEmbedField{Name: "Giá vàng", Value: r.goldSummary(ctx)})
	return embed
}

func (r *Router) resultHelp() string {
	p := r.prefix
	return strings.Join([]string{
		"`/xsmb` — kết quả mới nhất, trước 18h35 thì trả hôm qua",
		"`/xsmb ngay:14/08/2026` — một ngày, nhận cả `14-08-2026` và `2026-08-14`",
		"`/thongbao trangthai:bật` — bật thông báo 18h35 cho kênh này",
		"`/quaythu` — quay thử một bảng cho vui: số ngẫu nhiên, không lưu vào kho",
		"_prefix: `" + p + "`, `" + p + " 14/08/2026`, `" + p + " sub`_",
	}, "\n")
}

func (r *Router) statsHelp() string {
	p := r.prefix
	return strings.Join([]string{
		"`/thongke logan` — lô lâu chưa về nhất, kèm kỷ lục gan",
		"`/thongke degan` — như trên nhưng chỉ tính giải đặc biệt",
		"`/thongke tanso ngay:90` — tần suất, mặc định 30 ngày",
		"`/thongke tanso kieu:đầu` — gom 100 số thành 10 ô: đầu, đuôi, tổng, chạm",
		"`/thongke lo so:88` — hồ sơ đầy đủ một số",
		"`/thongke ngay ngay:03/09/2026` — phân tích một kỳ: kép, nháy, câm, chạm",
		"`/thongke db thang:08/2026` — bảng giải đặc biệt cả tháng",
		"`/thongke kho` — kho dữ liệu đang có gì",
		"_prefix: `" + p + " logan`, `" + p + " lo 88`, …_",
	}, "\n")
}

// goldSummary is the gold section of the combined help, kept short; the live
// list of codes lives in the gold-only help.
func (r *Router) goldSummary(ctx context.Context) string {
	lines := []string{
		"`/gold` — giá vàng trong nước và thế giới",
		"`/bieudo ma:DOHNL ngay:7` — biểu đồ, mặc định vàng miếng SJC 30 ngày",
	}
	if codes := r.liveGoldCodes(ctx); len(codes) > 0 {
		shown := codes
		if len(shown) > 6 {
			shown = shown[:6]
		}
		lines = append(lines, "Mã: `"+strings.Join(shown, "`, `")+"` … xem hết bằng `/huongdan phan:giá vàng`")
	}
	lines = append(lines, "_prefix: `"+r.goldPrefix+"`, `"+r.goldPrefix+" chart`_")
	return strings.Join(lines, "\n")
}

// explain turns a service error into something readable. "Chưa có" and
// "không có" are different answers.
func (r *Router) explain(err error) *discordgo.MessageEmbed {
	switch {
	case errors.Is(err, domain.ErrNotYet):
		return NoticeEmbed("Chưa có kết quả",
			"XSMB quay xong toàn bộ 27 số vào khoảng **18h35**. Thử lại sau ít phút nhé.", false)
	case errors.Is(err, domain.ErrNoResult):
		return NoticeEmbed("Không có kết quả", trimPrefix(err)+" không có kỳ quay nào.", false)
	case errors.Is(err, domain.ErrOutOfRange):
		return NoticeEmbed("Ngày không hợp lệ",
			fmt.Sprintf("Kho dữ liệu bắt đầu từ %s và không có ngày trong tương lai.",
				domain.FormatVN(domain.FirstDraw)), false)
	case errors.Is(err, provider.ErrBlocked):
		r.log.Error("upstream blocked the crawler", "error", err)
		return NoticeEmbed("Bị chặn",
			"xoso.com.vn đang chặn bot. Mình không lấy được kết quả lúc này.", true)
	default:
		r.log.Error("command failed", "error", err)
		return NoticeEmbed("Lỗi", "Không lấy được kết quả. Thử lại sau nhé.", true)
	}
}

// trimPrefix pulls the date back out of a wrapped error for display.
func trimPrefix(err error) string {
	text := err.Error()
	if at := strings.Index(text, ":"); at > 0 {
		return "Ngày " + text[:at]
	}
	return "Ngày này"
}
