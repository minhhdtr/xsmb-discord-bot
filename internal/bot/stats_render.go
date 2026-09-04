package bot

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/format"
)

const colourStats = 0x6b46c1

// statsFooter goes under every statistic. These figures describe what has
// happened, not what will.
const statsFooter = "Thống kê mô tả · mỗi kỳ quay độc lập với các kỳ trước"

// GanEmbed renders a drought ranking. The record column is what makes the
// current figure readable; * marks a run past its own record.
func GanEmbed(title string, entries []domain.Gan, archive int, asOf string) *discordgo.MessageEmbed {
	if len(entries) == 0 {
		return NoticeEmbed("Chưa có dữ liệu",
			"Kho chưa có kỳ quay nào để thống kê. Chờ backfill chạy xong nhé.", false)
	}
	var b strings.Builder
	b.WriteString("Số   Ngày  Lần cuối    Kỷ lục\n")
	for _, g := range entries {
		mark := " "
		if g.NewRecord() {
			mark = "*"
		}
		fmt.Fprintf(&b, "%-3s %5d  %-10s %5d%s\n",
			g.Number, g.Days, domain.FormatVN(g.LastSeen), g.Record, mark)
	}

	embed := &discordgo.MessageEmbed{
		Title:       title + " · tính đến " + asOf,
		Color:       colourStats,
		Description: "```\n" + strings.TrimRight(b.String(), "\n") + "\n```",
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Kho %s kỳ · %s", format.Decimal(float64(archive), 0), statsFooter),
		},
	}
	for _, g := range entries {
		if g.NewRecord() {
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
				Name:  "Dấu *",
				Value: "Đang vượt kỷ lục gan của chính số đó.",
			})
			break
		}
	}
	return embed
}

// FrequencyEmbed shows the busiest and quietest numbers over a window.
func FrequencyEmbed(freq []domain.Frequency, days, archive int) *discordgo.MessageEmbed {
	if len(freq) == 0 {
		return NoticeEmbed("Chưa có dữ liệu", "Kho chưa có kỳ quay nào để thống kê.", false)
	}
	const show = 10
	window := fmt.Sprintf("%d ngày", days)
	if days <= 0 {
		window = "toàn kho"
	}

	top := freq
	if len(top) > show {
		top = top[:show]
	}
	bottom := freq
	if len(bottom) > show {
		bottom = bottom[len(bottom)-show:]
	}

	return &discordgo.MessageEmbed{
		Title: "📊 Tần suất lô · " + window,
		Color: colourStats,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Về nhiều nhất", Value: freqColumn(top), Inline: true},
			{Name: "Về ít nhất", Value: freqColumn(reverse(bottom)), Inline: true},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Đếm nháy · kho %s kỳ · %s",
				format.Decimal(float64(archive), 0), statsFooter),
		},
	}
}

// freqColumn renders one side of the frequency table.
func freqColumn(freq []domain.Frequency) string {
	var b strings.Builder
	b.WriteString("```\n")
	for _, f := range freq {
		fmt.Fprintf(&b, "%-3s %3d nháy\n", f.Number, f.Hits)
	}
	b.WriteString("```")
	return b.String()
}

func reverse(in []domain.Frequency) []domain.Frequency {
	out := make([]domain.Frequency, len(in))
	for i, f := range in {
		out[len(in)-1-i] = f
	}
	return out
}

// ProfileEmbed renders one number's history.
func ProfileEmbed(p domain.Profile) *discordgo.MessageEmbed {
	if p.Archive == 0 {
		return NoticeEmbed("Chưa có dữ liệu", "Kho chưa có kỳ quay nào để thống kê.", false)
	}
	var head strings.Builder
	head.WriteString("```\n")
	if p.Gan.LastSeen.IsZero() {
		head.WriteString("Chưa từng về trong kho\n")
	} else {
		fmt.Fprintf(&head, "%-14s%s · %d ngày trước\n", "Lần cuối về",
			domain.FormatVN(p.Gan.LastSeen), p.Gan.Days)
		fmt.Fprintf(&head, "%-14s%d ngày\n", "Đang gan", p.Gan.Days)
		if p.Gan.Record > 0 {
			fmt.Fprintf(&head, "%-14s%d ngày, hết ngày %s\n", "Kỷ lục gan",
				p.Gan.Record, domain.FormatVN(p.Gan.RecordEnd))
		}
		if p.AvgCycle > 0 {
			fmt.Fprintf(&head, "%-14s%s ngày\n", "Chu kỳ TB", format.Decimal(p.AvgCycle, 1))
		}
		fmt.Fprintf(&head, "%-14s%s\n", "Về lần đầu", domain.FormatVN(p.First))
	}
	head.WriteString("```")

	var windows strings.Builder
	windows.WriteString("```\n")
	for _, w := range p.Windows {
		fmt.Fprintf(&windows, "%-9s %4d nháy · %4d/%d kỳ\n", w.Label, w.Hits, w.Draws, w.Total)
	}
	windows.WriteString("```")

	embed := &discordgo.MessageEmbed{
		Title:       "🔎 Lô " + p.Number,
		Color:       colourStats,
		Description: head.String(),
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Tần suất", Value: windows.String()},
		},
		Footer: &discordgo.MessageEmbedFooter{Text: statsFooter},
	}
	if name, strip := recentStrip(p.Recent); strip != "" {
		embed.Fields = append(embed.Fields,
			&discordgo.MessageEmbedField{Name: name, Value: strip})
	}
	if p.Gan.NewRecord() {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:  "Đang vượt kỷ lục",
			Value: fmt.Sprintf("Gan %d ngày, dài hơn kỷ lục cũ %d ngày.", p.Gan.Days, p.Gan.Record),
		})
	}
	return embed
}

// SpecialMonthEmbed lays a month into two columns to halve the height.
func SpecialMonthEmbed(days []domain.SpecialDay, year int, month int) *discordgo.MessageEmbed {
	label := fmt.Sprintf("%02d/%04d", month, year)
	if len(days) == 0 {
		return NoticeEmbed("Không có dữ liệu",
			fmt.Sprintf("Kho chưa có kỳ quay nào trong tháng %s.", label), false)
	}

	half := (len(days) + 1) / 2
	var b strings.Builder
	b.WriteString("Ngày ĐB    Đề   Ngày ĐB    Đề\n")
	for i := 0; i < half; i++ {
		b.WriteString(specialCell(days[i]))
		if j := i + half; j < len(days) {
			b.WriteString("  " + specialCell(days[j]))
		}
		b.WriteByte('\n')
	}

	return &discordgo.MessageEmbed{
		Title:       "🎱 Giải đặc biệt tháng " + label,
		Color:       colourStats,
		Description: "```\n" + strings.TrimRight(b.String(), "\n") + "\n```",
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("%d kỳ · %s", len(days), statsFooter),
		},
	}
}

// recentStrip draws one character per recent draw: the hit count, or a dot
// for a day the number missed. Concrete and checkable, unlike a ranking.
func recentStrip(days []domain.DayHit) (name, value string) {
	if len(days) == 0 {
		return "", ""
	}
	var b strings.Builder
	for _, d := range days {
		switch {
		case d.Hits <= 0:
			b.WriteString("·")
		case d.Hits > 9:
			b.WriteString("+")
		default:
			b.WriteString(strconv.Itoa(d.Hits))
		}
	}
	name = fmt.Sprintf("%d kỳ gần nhất · %s → %s", len(days),
		domain.FormatVN(days[0].Day), domain.FormatVN(days[len(days)-1].Day))
	return name, "```\n" + b.String() + "\n```· = không về · số = số nháy"
}

func specialCell(d domain.SpecialDay) string {
	return fmt.Sprintf("%02d   %-6s %s", d.Day.Day(), d.Special, d.De)
}

// DayReportEmbed renders the descriptive read of one draw: what doubled up,
// what came up empty, what the special prize touches. The counts table is one
// code block so the columns line up on a phone.
func DayReportEmbed(draw domain.Draw) *discordgo.MessageEmbed {
	report := draw.Prizes.Report()
	if report.De == "" {
		return NoticeEmbed("Không có dữ liệu",
			"Kỳ quay này chưa đủ 27 số để phân tích.", false)
	}

	var b strings.Builder
	b.WriteString("      0  1  2  3  4  5  6  7  8  9\n")
	b.WriteString(digitRow("Đầu", report.Heads))
	b.WriteString(digitRow("Đuôi", report.Tails))

	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("📊 Phân tích XSMB · %s %s",
			domain.WeekdayVN(draw.Date), domain.FormatVN(draw.Date)),
		Color: colourStats,
		Description: fmt.Sprintf("**Đề · %s** — chạm đầu **%d**, chạm đuôi **%d**, tổng **%d**\n```\n%s\n```",
			report.De, report.ChamDau, report.ChamDuoi, report.TongDe,
			strings.TrimRight(b.String(), "\n")),
		Footer: &discordgo.MessageEmbedFooter{Text: statsFooter},
	}

	embed.Fields = []*discordgo.MessageEmbedField{
		{Name: "Lô kép", Value: joinOrDash(report.Kep), Inline: true},
		{Name: "Nháy", Value: nhayList(report.Nhay), Inline: true},
		{Name: "Về nhiều nhất", Value: fmt.Sprintf("đầu %s · đuôi %s",
			digitList(report.TopHeads), digitList(report.TopTails)), Inline: true},
		{Name: "Câm", Value: fmt.Sprintf("đầu %s · đuôi %s",
			digitList(report.MuteHeads), digitList(report.MuteTails)), Inline: false},
	}
	return embed
}

// digitRow is one line of the counts table, padded to the header above it.
func digitRow(label string, counts [10]int) string {
	var b strings.Builder
	b.WriteString(padRunes(label, 5))
	for _, n := range counts {
		fmt.Fprintf(&b, "%2d ", n)
	}
	return strings.TrimRight(b.String(), " ") + "\n"
}

// digitList renders a set of digits, or a dash when the set is empty. "Không
// có" would be truthful but too wide for an inline field.
func digitList(digits []int) string {
	if len(digits) == 0 {
		return "—"
	}
	parts := make([]string, len(digits))
	for i, d := range digits {
		parts[i] = strconv.Itoa(d)
	}
	return strings.Join(parts, " ")
}

func joinOrDash(values []string) string {
	if len(values) == 0 {
		return "—"
	}
	return strings.Join(values, " ")
}

// nhayList names the multiplicity rather than printing a number, since "hai
// nháy" is what a person says out loud.
func nhayList(entries []domain.Nhay) string {
	if len(entries) == 0 {
		return "—"
	}
	names := map[int]string{2: "hai", 3: "ba", 4: "bốn", 5: "năm"}
	parts := make([]string, len(entries))
	for i, e := range entries {
		word, ok := names[e.Hits]
		if !ok {
			word = strconv.Itoa(e.Hits)
		}
		parts[i] = fmt.Sprintf("%s (%s nháy)", e.Number, word)
	}
	return strings.Join(parts, "\n")
}
