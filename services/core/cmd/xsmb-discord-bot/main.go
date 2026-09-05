// Command xsmb-discord-bot serves XSMB results and gold prices to Discord.
//
//	xsmb-discord-bot                    run the bot
//	xsmb-discord-bot fetch [date]       crawl one day and print it
//	xsmb-discord-bot backfill [from]    fill the archive, then exit
//	xsmb-discord-bot gold               print the gold board and exit
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"github.com/minhhdtr/xsmb-discord-bot/internal/bot"
	"github.com/minhhdtr/xsmb-discord-bot/internal/config"
	"github.com/minhhdtr/xsmb-discord-bot/internal/coreclient"
	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/httpapi"
	"github.com/minhhdtr/xsmb-discord-bot/internal/present"
	"github.com/minhhdtr/xsmb-discord-bot/internal/provider"
	"github.com/minhhdtr/xsmb-discord-bot/internal/service"
	"github.com/minhhdtr/xsmb-discord-bot/internal/storage"
)

func main() {
	var err error
	switch {
	case len(os.Args) > 1 && os.Args[1] == "fetch":
		err = runFetch(os.Args[2:])
	case len(os.Args) > 1 && os.Args[1] == "backfill":
		err = runBackfill(os.Args[2:])
	case len(os.Args) > 1 && os.Args[1] == "gold":
		err = runGold()
	default:
		err = run()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "lỗi:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg.LogLevel)

	// Ctrl-C and SIGTERM cancel the root context; every goroutine hangs off it.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, svc, err := open(ctx, cfg.DatabaseURL, cfg.SourceBaseURL, log)
	if err != nil {
		return err
	}
	defer store.Close()
	log.Info("database ready")

	goldOpts := []provider.GoldOption{provider.WithGoldLogger(log)}
	if cfg.GoldURL != "" {
		goldOpts = append(goldOpts, provider.WithGoldURL(cfg.GoldURL))
	}
	gold := service.NewGold(provider.NewVangToday(goldOpts...), cfg.GoldTTL, cfg.GoldGrace, nil, log)

	// Ingest runs whether or not any channel is subscribed. Announcing is the
	// bot's job and a separate one; the archive must not depend on it.
	go service.NewIngest(svc, log).Run(ctx)

	// The API has to be listening before the bot dials it, since every command
	// now goes over the wire.
	ready := make(chan struct{})
	go func() {
		close(ready)
		if err := httpapi.New(svc, store, gold, log).Listen(cfg.APIAddr, ctx.Done()); err != nil {
			log.Error("core api stopped", "error", err)
		}
	}()
	<-ready

	// The bot reaches core over HTTP even though both are in this binary. The
	// extra hop buys nothing today; what it buys is that the contract is being
	// exercised by a client whose behaviour is already known, before the same
	// contract has to be read by a second language.
	core := coreclient.New(cfg.CoreURL, 0)

	b, err := bot.New(cfg.DiscordToken, bot.Options{
		Prefix:         cfg.Prefix,
		GoldPrefix:     cfg.GoldPrefix,
		GuildID:        cfg.GuildID,
		PrefixCommands: cfg.PrefixCommands,
	}, core, svc.Now, log)
	if err != nil {
		return err
	}

	if cfg.BackfillOnStart {
		go backgroundBackfill(ctx, cfg, svc, log)
	}

	log.Info("starting bot", "prefix", cfg.Prefix, "gold_prefix", cfg.GoldPrefix,
		"prefix_commands", cfg.PrefixCommands,
		"draw_time", fmt.Sprintf("%02d:%02d", domain.CompleteHour, domain.CompleteMinute))
	return b.Run(ctx)
}

// backgroundBackfill tops up the archive without blocking the gateway. Starts
// late so the bot answers commands first.
func backgroundBackfill(ctx context.Context, cfg config.Config, svc *service.Service, log *slog.Logger) {
	const settle = 15 * time.Second
	select {
	case <-ctx.Done():
		return
	case <-time.After(settle):
	}

	from := cfg.BackfillStart(svc.Now())
	log.Info("backfill starting in background",
		"from", domain.FormatISO(from), "concurrency", cfg.BackfillConcurrency)

	report := svc.Backfill(ctx, service.BackfillOptions{
		From:        from,
		Concurrency: cfg.BackfillConcurrency,
		Rate:        cfg.BackfillRate,
	}, logEvery(log, 250))

	switch {
	case ctx.Err() != nil:
		log.Info("backfill cancelled", "progress", report.String())
	case report.Aborted:
		log.Error("backfill aborted", "progress", report.String(), "error", report.Err)
	default:
		log.Info("backfill finished", "result", report.String())
	}
}

// logEvery reports progress at intervals so a long run doesn't drown the log.
func logEvery(log *slog.Logger, n int) service.Progress {
	return func(day time.Time, report service.Report) {
		if report.Scanned%n == 0 {
			log.Info("backfill progress", "at", domain.FormatISO(day), "progress", report.String())
		}
	}
}

func runBackfill(args []string) error {
	flags := flag.NewFlagSet("backfill", flag.ContinueOnError)
	fromFlag := flags.String("from", "", "ngày bắt đầu, ví dụ 01/10/2005 (mặc định: BACKFILL_DAYS ngày gần nhất)")
	toFlag := flags.String("to", "", "ngày kết thúc (mặc định: kỳ mới nhất)")
	rateFlag := flags.Duration("rate", 0, "khoảng nghỉ giữa hai request; 0 là không nghỉ")
	workersFlag := flags.Int("concurrency", 0, "số ngày lấy song song (mặc định: BACKFILL_CONCURRENCY)")
	if err := flags.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		// A backfill needs the database but not a token.
		if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
			cfg = config.Config{DatabaseURL: dsn, LogLevel: os.Getenv("LOG_LEVEL"),
				SourceBaseURL: os.Getenv("XOSO_BASE_URL"), BackfillConcurrency: 8}
		} else {
			return err
		}
	}
	log := newLogger(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, svc, err := open(ctx, cfg.DatabaseURL, cfg.SourceBaseURL, log)
	if err != nil {
		return err
	}
	defer store.Close()

	opts := service.BackfillOptions{
		From:        cfg.BackfillStart(svc.Now()),
		Concurrency: cfg.BackfillConcurrency,
		Rate:        cfg.BackfillRate,
	}
	if *rateFlag > 0 {
		opts.Rate = *rateFlag
	}
	if *workersFlag > 0 {
		opts.Concurrency = *workersFlag
	}
	if *fromFlag != "" {
		if opts.From, err = domain.ParseDate(*fromFlag, svc.Now()); err != nil {
			return err
		}
	}
	if *toFlag != "" {
		if opts.To, err = domain.ParseDate(*toFlag, svc.Now()); err != nil {
			return err
		}
	}

	// Resolve To for the log so an unset value doesn't print as the zero date.
	shownTo := domain.FormatISO(domain.LatestPublished(svc.Now()))
	if !opts.To.IsZero() {
		shownTo = domain.FormatISO(opts.To)
	}
	started := time.Now()
	log.Info("backfill starting", "from", domain.FormatISO(opts.From),
		"to", shownTo, "concurrency", opts.Concurrency)
	report := svc.Backfill(ctx, opts, logEvery(log, 250))
	fmt.Printf("xong sau %s\n", time.Since(started).Round(time.Second))
	fmt.Println(report.String())
	if report.Aborted && ctx.Err() == nil {
		return report.Err
	}
	return nil
}

// runFetch crawls one day and prints it. Run this first on a new machine.
func runFetch(args []string) error {
	log := newLogger(envOr("LOG_LEVEL", "info"))
	now := time.Now().In(domain.Location())

	day := domain.LatestPublished(now)
	if len(args) > 0 {
		parsed, err := domain.ParseDate(args[0], now)
		if err != nil {
			return err
		}
		day = parsed
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	svc := service.New(storage.NewMemory(), newProvider(os.Getenv("XOSO_BASE_URL"), log), nil, log)
	draw, err := svc.Get(ctx, day)
	if err != nil {
		return err
	}
	fmt.Printf("XSMB %s %s  ·  nguồn %s\n\n",
		domain.WeekdayVN(draw.Date), domain.FormatVN(draw.Date), draw.Source)
	fmt.Println(present.Table(draw.Prizes))
	fmt.Println()
	fmt.Println(present.HeadTail(draw.Prizes))
	return nil
}

// runGold prints the current board. No Discord, no database.
func runGold() error {
	log := newLogger(envOr("LOG_LEVEL", "info"))
	opts := []provider.GoldOption{provider.WithGoldLogger(log)}
	if url := os.Getenv("GOLD_URL"); url != "" {
		opts = append(opts, provider.WithGoldURL(url))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	board, err := provider.NewVangToday(opts...).Board(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Giá vàng · %s · nguồn %s\n\n",
		board.UpdatedAt.Format("15:04 02/01/2006"), board.Source)
	if world, ok := board.World(); ok {
		fmt.Printf("%-22s %12.1f USD/oz\n\n", world.Name, world.Buy)
	}
	fmt.Printf("%-22s %12s %12s\n", "Loại", "Mua", "Bán")
	for _, q := range board.Domestic() {
		fmt.Printf("%-22s %12.0f %12.0f\n", q.Name, q.Buy/1000, q.Sell/1000)
	}
	fmt.Println("\nđơn vị: nghìn đồng/lượng")
	return nil
}

func open(ctx context.Context, dsn, baseURL string, log *slog.Logger) (*storage.Postgres, *service.Service, error) {
	openCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	store, err := storage.OpenPostgres(openCtx, dsn)
	if err != nil {
		return nil, nil, err
	}
	return store, service.New(store, newProvider(baseURL, log), nil, log), nil
}

func newProvider(baseURL string, log *slog.Logger) *provider.Xoso {
	opts := []provider.Option{provider.WithLogger(log)}
	if baseURL != "" {
		log.Warn("using a non-default source", "base_url", baseURL)
		opts = append(opts, provider.WithBaseURL(baseURL))
	}
	return provider.NewXoso(opts...)
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l}))
}
