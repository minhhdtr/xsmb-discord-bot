package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// Slash commands are a second way into the same router. Match/Dispatch already
// separate routing from Discord's plumbing, so this file only translates an
// interaction into the Request the prefix path already produces.

// manageChannels is the permission Discord itself enforces on /thongbao.
var manageChannels int64 = discordgo.PermissionManageChannels

// exampleDates fill the "ví dụ ..." part of the option descriptions. Written
// into the source they age badly - a hardcoded 2026 still reads 2026 in 2028 -
// so they are derived from the clock instead. They are fixed when the commands
// are registered, which happens once per start.
func exampleDates(now time.Time) (day, month string) {
	today := domain.DayOf(now)
	// Subtracting the day-of-month lands on the last day of the previous
	// month, without AddDate's month-length surprises.
	previous := today.AddDate(0, 0, -today.Day())
	return domain.FormatVN(domain.LatestPublished(now)),
		domain.FormatMonth(previous.Year(), previous.Month())
}

// SlashCommands is the set registered with Discord. now supplies the example
// dates in the descriptions.
func SlashCommands(now time.Time) []*discordgo.ApplicationCommand {
	exampleDay, exampleMonth := exampleDates(now)
	return []*discordgo.ApplicationCommand{
		{
			Name:        "xsmb",
			Description: "Kết quả xổ số miền Bắc",
			Options: []*discordgo.ApplicationCommandOption{{
				Type: discordgo.ApplicationCommandOptionString,
				Name: "ngay",
				Description: fmt.Sprintf(
					"Ngày cần xem, ví dụ %s. Bỏ trống thì lấy kỳ mới nhất.", exampleDay),
			}},
		},
		{
			Name:        "gold",
			Description: "Giá vàng trong nước và thế giới",
		},
		{
			Name:        "bieudo",
			Description: "Biểu đồ giá vàng",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:         discordgo.ApplicationCommandOptionString,
					Name:         "ma",
					Description:  "Mã vàng. Gõ để lọc, bỏ trống thì lấy vàng miếng SJC.",
					Autocomplete: true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionInteger,
					Name:        "ngay",
					Description: "Số ngày, tối đa 30",
					MinValue:    ptrFloat(2),
					MaxValue:    30,
				},
			},
		},
		{
			Name:        "thongke",
			Description: "Thống kê từ kho kết quả",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "logan",
					Description: "Lô lâu chưa về nhất, kèm kỷ lục gan",
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "degan",
					Description: "Như logan nhưng chỉ tính giải đặc biệt",
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "tanso",
					Description: "Tần suất về của cả 100 số",
					Options: []*discordgo.ApplicationCommandOption{
						{
							Type:        discordgo.ApplicationCommandOptionInteger,
							Name:        "ngay",
							Description: "Cửa sổ tính, mặc định 30 ngày",
							MinValue:    ptrFloat(1),
						},
						{
							Type:        discordgo.ApplicationCommandOptionString,
							Name:        "kieu",
							Description: "Gom 100 số thành 10 ô. Bỏ trống thì liệt kê từng số.",
							Choices: []*discordgo.ApplicationCommandOptionChoice{
								{Name: "đầu", Value: "dau"},
								{Name: "đuôi", Value: "duoi"},
								{Name: "tổng", Value: "tong"},
								{Name: "chạm", Value: "cham"},
							},
						},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "lo",
					Description: "Hồ sơ đầy đủ của một số",
					Options: []*discordgo.ApplicationCommandOption{{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "so",
						Description: "Số hai chữ số, ví dụ 88",
						Required:    true,
					}},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "ngay",
					Description: "Phân tích một kỳ: kép, nháy, đầu đuôi câm, chạm",
					Options: []*discordgo.ApplicationCommandOption{{
						Type: discordgo.ApplicationCommandOptionString,
						Name: "ngay",
						Description: fmt.Sprintf(
							"Ngày cần xem, ví dụ %s. Bỏ trống thì lấy kỳ mới nhất.", exampleDay),
					}},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "db",
					Description: "Bảng giải đặc biệt cả tháng",
					Options: []*discordgo.ApplicationCommandOption{{
						Type: discordgo.ApplicationCommandOptionString,
						Name: "thang",
						Description: fmt.Sprintf(
							"Tháng cần xem, ví dụ %s. Bỏ trống thì lấy tháng này.", exampleMonth),
					}},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "kho",
					Description: "Kho dữ liệu đang có gì",
				},
			},
		},
		{
			Name:                     "thongbao",
			Description:              "Bật hoặc tắt thông báo kết quả lúc 18h35 cho kênh này",
			DefaultMemberPermissions: &manageChannels,
			Options: []*discordgo.ApplicationCommandOption{{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "trangthai",
				Description: "Bật hay tắt",
				Required:    true,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "bật", Value: "sub"},
					{Name: "tắt", Value: "unsub"},
				},
			}},
		},
		{
			Name:        "quaythu",
			Description: "Quay thử một bảng XSMB cho vui — số ngẫu nhiên, không phải kết quả thật",
		},
		{
			Name:        "huongdan",
			Description: "Danh sách lệnh",
			Options: []*discordgo.ApplicationCommandOption{{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "phan",
				Description: "Chỉ xem một phần. Bỏ trống thì liệt kê tất cả.",
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "xổ số", Value: "xsmb"},
					{Name: "giá vàng", Value: "vang"},
				},
			}},
		},
	}
}

// SlashRequest translates an interaction into the arguments the router already
// understands. Keeping one router means slash and prefix cannot drift apart.
// Exported so the mapping can be tested without a session.
func SlashRequest(data discordgo.ApplicationCommandInteractionData) (kind Kind, args []string, ephemeral bool) {
	switch data.Name {
	case "xsmb":
		if day := optString(data.Options, "ngay"); day != "" {
			return KindLottery, strings.Fields(day), false
		}
		return KindLottery, nil, false

	case "gold":
		return KindGold, nil, false

	case "bieudo":
		args = []string{"chart"}
		if code := optString(data.Options, "ma"); code != "" {
			args = append(args, code)
		}
		if days, ok := optInt(data.Options, "ngay"); ok {
			args = append(args, strconv.Itoa(days))
		}
		return KindGold, args, false

	case "thongke":
		if len(data.Options) == 0 {
			return KindLottery, []string{"help"}, true
		}
		sub := data.Options[0]
		switch sub.Name {
		case "tanso":
			args = []string{"tanso"}
			if days, ok := optInt(sub.Options, "ngay"); ok {
				args = append(args, strconv.Itoa(days))
			}
			if kind := optString(sub.Options, "kieu"); kind != "" {
				args = append(args, kind)
			}
		case "lo":
			args = []string{"lo", optString(sub.Options, "so")}
		case "ngay":
			args = []string{"ngay"}
			if day := optString(sub.Options, "ngay"); day != "" {
				args = append(args, strings.Fields(day)...)
			}
		case "db":
			args = []string{"db"}
			if month := optString(sub.Options, "thang"); month != "" {
				args = append(args, month)
			}
		case "kho":
			args = []string{"status"}
		default:
			args = []string{sub.Name}
		}
		return KindLottery, args, false

	case "quaythu":
		return KindLottery, []string{"quaythu"}, false

	case "thongbao":
		return KindLottery, []string{optString(data.Options, "trangthai")}, true

	case "huongdan":
		args = []string{"help"}
		if section := optString(data.Options, "phan"); section != "" {
			args = append(args, section)
		}
		return KindLottery, args, true
	}
	return KindNone, nil, false
}

func optString(opts []*discordgo.ApplicationCommandInteractionDataOption, name string) string {
	for _, o := range opts {
		if o.Name == name {
			if s, ok := o.Value.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func optInt(opts []*discordgo.ApplicationCommandInteractionDataOption, name string) (int, bool) {
	for _, o := range opts {
		if o.Name == name {
			if f, ok := o.Value.(float64); ok {
				return int(f), true
			}
		}
	}
	return 0, false
}

func ptrFloat(v float64) *float64 { return &v }

// onInteraction answers a slash command.
//
// Discord drops an interaction that is not acknowledged within three seconds,
// and a cold statistic or a first crawl of a missing day takes longer than
// that. So every command is deferred first and the real answer edited in.
func (b *Bot) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type == discordgo.InteractionApplicationCommandAutocomplete {
		b.onAutocomplete(s, i)
		return
	}
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}
	kind, args, ephemeral := SlashRequest(i.ApplicationCommandData())
	if kind == KindNone {
		return
	}

	var flags discordgo.MessageFlags
	if ephemeral {
		flags = discordgo.MessageFlagsEphemeral
	}
	// The flag has to be set here; a deferred reply cannot become ephemeral
	// later.
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: flags},
	}); err != nil {
		b.log.Error("cannot acknowledge interaction", "command", i.ApplicationCommandData().Name, "error", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	reply := b.router.Dispatch(ctx, kind, Request{
		Args:      args,
		GuildID:   i.GuildID,
		ChannelID: i.ChannelID,
		CanManage: interactionCanManage(i),
	})

	edit := &discordgo.WebhookEdit{Embeds: &[]*discordgo.MessageEmbed{reply.Embed}}
	if reply.File != nil {
		edit.Files = []*discordgo.File{reply.File}
	}
	if _, err := s.InteractionResponseEdit(i.Interaction, edit); err != nil {
		b.log.Error("cannot send interaction reply", "command", i.ApplicationCommandData().Name, "error", err)
		return
	}

	// Interaction edits go to a webhook route, which is a different rate limit
	// bucket from editing messages in the channel. Two spins in one channel
	// therefore do not queue behind each other. The token lasts fifteen
	// minutes, far longer than any frame sequence.
	b.playFrames(reply, func(frame *discordgo.MessageEmbed) error {
		_, err := s.InteractionResponseEdit(i.Interaction,
			&discordgo.WebhookEdit{Embeds: &[]*discordgo.MessageEmbed{frame}})
		return err
	})
}

// autocompleteTimeout is tight because Discord expects suggestions while the
// user is still typing, and there is no way to defer one.
const autocompleteTimeout = 2 * time.Second

// onAutocomplete suggests gold codes as the user types.
//
// The suggestions come from the live board rather than a list in the source,
// for the same reason /gold help does: a brand added upstream should be
// offered, and one dropped upstream should stop being offered. The built-in
// list is only the fallback for when the source is slow or down.
func (b *Bot) onAutocomplete(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	if data.Name != "bieudo" {
		return
	}
	typed := ""
	for _, o := range data.Options {
		if o.Focused && o.Name == "ma" {
			typed, _ = o.Value.(string)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), autocompleteTimeout)
	defer cancel()

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{Choices: b.router.GoldChoices(ctx, typed)},
	}); err != nil {
		b.log.Warn("cannot answer autocomplete", "error", err)
	}
}

// interactionCanManage reads the permissions Discord already computed for the
// caller in this channel. Outside a guild there is nothing to manage.
func interactionCanManage(i *discordgo.InteractionCreate) bool {
	if i.GuildID == "" {
		return true
	}
	if i.Member == nil {
		return false
	}
	const mask = discordgo.PermissionManageChannels | discordgo.PermissionAdministrator
	return i.Member.Permissions&mask != 0
}

// registerSlashCommands replaces the whole command set in one call, so a
// renamed command does not leave the old name behind.
//
// With a guild id the commands appear immediately, which is what you want
// while developing; without one they are global and can take up to an hour to
// propagate.
func (b *Bot) registerSlashCommands(appID, guildID string) error {
	_, err := b.session.ApplicationCommandBulkOverwrite(appID, guildID,
		SlashCommands(b.router.now()))
	return err
}
