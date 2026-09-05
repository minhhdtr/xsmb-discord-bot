package bot

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/bwmarrin/discordgo"
)

// commandTimeout bounds one command, including a cold crawl of the source.
const commandTimeout = 30 * time.Second

// Bot is the Discord gateway client.
type Bot struct {
	session   *discordgo.Session
	router    *Router
	announcer *Announcer
	log       *slog.Logger

	guildID string // slash commands go here when set, globally when not
	prefix  bool   // whether the !xsmb form is answered as well
}

// Options tunes a Bot.
type Options struct {
	Prefix     string
	GoldPrefix string
	// GuildID registers slash commands to one server, where they appear at
	// once instead of taking up to an hour.
	GuildID string
	// PrefixCommands answers the !xsmb form too. Off means the Message
	// Content intent is not requested, so the app needs no privileged intent.
	PrefixCommands bool
}

// New builds a Bot. The application needs the Message Content intent, or
// message bodies arrive empty and no command is ever seen.
func New(token string, opts Options, core Core, clock func() time.Time, log *slog.Logger) (*Bot, error) {
	if log == nil {
		log = slog.Default()
	}
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	// Message Content is privileged. Requesting it when the application has
	// not been granted it makes the gateway refuse the connection outright,
	// so it is only asked for when prefix commands are actually wanted.
	session.Identify.Intents = discordgo.IntentsGuilds
	if opts.PrefixCommands {
		session.Identify.Intents |= discordgo.IntentsGuildMessages |
			discordgo.IntentsDirectMessages |
			discordgo.IntentMessageContent
	}

	b := &Bot{
		session: session,
		router:  NewRouter(core, clock, opts.Prefix, opts.GoldPrefix, log),
		log:     log,
		guildID: opts.GuildID,
		prefix:  opts.PrefixCommands,
	}
	b.announcer = NewAnnouncer(core, clock, b.postEmbed, log)

	session.AddHandler(b.onReady)
	session.AddHandler(b.onInteraction)
	if opts.PrefixCommands {
		session.AddHandler(b.onMessage)
	}
	return b, nil
}

// Run opens the gateway, starts the announcer and blocks until ctx ends.
func (b *Bot) Run(ctx context.Context) error {
	if err := b.session.Open(); err != nil {
		return fmt.Errorf("open gateway: %w", err)
	}
	defer b.session.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		b.announcer.Run(ctx)
	}()

	<-ctx.Done()
	b.log.Info("shutting down")
	<-done
	return nil
}

func (b *Bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	b.log.Info("connected to Discord", "user", r.User.Username,
		"guilds", len(r.Guilds), "prefix_commands", b.prefix)

	scope := "global"
	if b.guildID != "" {
		scope = "guild " + b.guildID
	}
	if err := b.registerSlashCommands(r.User.ID, b.guildID); err != nil {
		b.log.Error("cannot register slash commands", "scope", scope, "error", err)
	} else {
		b.log.Info("slash commands registered", "scope", scope,
			"count", len(SlashCommands(b.router.now())))
	}

	if err := s.UpdateGameStatus(0, "/xsmb · kết quả XSMB"); err != nil {
		b.log.Warn("cannot set status", "error", err)
	}
}

func (b *Bot) onMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot {
		return // never answer ourselves or another bot
	}
	kind, args := b.router.Match(m.Content)
	if kind == KindNone {
		return
	}

	// A cold crawl takes a second or two; without this it looks ignored.
	_ = s.ChannelTyping(m.ChannelID)

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	reply := b.router.Dispatch(ctx, kind, Request{
		Args:      args,
		GuildID:   m.GuildID,
		ChannelID: m.ChannelID,
		CanManage: b.canManage(s, m),
	})

	message := &discordgo.MessageSend{Embed: reply.Embed}
	if reply.File != nil {
		message.Files = []*discordgo.File{reply.File}
	}
	sent, err := s.ChannelMessageSendComplex(m.ChannelID, message)
	if err != nil {
		b.log.Error("cannot send reply", "channel", m.ChannelID, "error", err)
		return
	}
	b.playFrames(reply, func(frame *discordgo.MessageEmbed) error {
		_, err := s.ChannelMessageEditEmbed(m.ChannelID, sent.ID, frame)
		return err
	})
}

// playFrames edits a message through the rest of a reply's frames, waiting
// Pace between each. It runs on the gateway handler's own goroutine, which
// discordgo already gives one of per event, so a spin holds up nothing else.
//
// A failed edit stops the playback. Carrying on would mean waiting out the
// remaining frames to show a board that never updates, and the usual cause -
// the message being deleted - will only fail again.
func (b *Bot) playFrames(reply Reply, edit func(*discordgo.MessageEmbed) error) {
	if len(reply.Frames) == 0 {
		return
	}
	pace := reply.Pace
	if pace <= 0 {
		pace = time.Second
	}
	for _, frame := range reply.Frames {
		time.Sleep(pace)
		if err := edit(frame); err != nil {
			b.log.Warn("stopped a frame sequence", "error", err)
			return
		}
	}
}

// canManage reports whether the author may change this channel's
// subscription. DMs have no permissions, so the author owns the conversation.
func (b *Bot) canManage(s *discordgo.Session, m *discordgo.MessageCreate) bool {
	if m.GuildID == "" {
		return true
	}
	perms, err := s.UserChannelPermissions(m.Author.ID, m.ChannelID)
	if err != nil {
		b.log.Warn("cannot read permissions", "user", m.Author.ID, "error", err)
		return false
	}
	const mask = discordgo.PermissionManageChannels | discordgo.PermissionAdministrator
	return perms&mask != 0
}

func (b *Bot) postEmbed(ctx context.Context, channelID string, embed *discordgo.MessageEmbed) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := b.session.ChannelMessageSendEmbed(channelID, embed)
	return err
}
