// Package config reads settings from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// Config is everything the bot needs to start.
type Config struct {
	DiscordToken string
	DatabaseURL  string
	Prefix       string
	LogLevel     string
	// SourceBaseURL overrides the site the crawler reads. Empty means default.
	SourceBaseURL string

	// BackfillOnStart runs a backfill in the background once the bot is up.
	BackfillOnStart bool
	// BackfillDays is how far back that background run reaches. Zero, the
	// default, means the whole archive.
	BackfillDays int
	// BackfillConcurrency is how many days are fetched at once.
	BackfillConcurrency int
	// BackfillFrom, when set, overrides BackfillDays with an explicit date.
	BackfillFrom time.Time
	// BackfillRate is the minimum gap between requests during a backfill.
	// Zero, the default, means no pacing.
	BackfillRate time.Duration

	// GoldPrefix is the command that shows gold prices. Empty disables it.
	GoldPrefix string
	// GoldURL is the price source.
	GoldURL string
	// GoldTTL is how long a fetched board is reused.
	GoldTTL time.Duration
	// GoldGrace is how long a stale board may be served while the source is
	// failing. A price from a few minutes ago beats an error message.
	GoldGrace time.Duration

	// GuildID registers slash commands to one server, where they appear at
	// once. Empty registers them globally, which can take an hour to spread.
	GuildID string
	// PrefixCommands keeps the !xsmb form working. Turning it off also drops
	// the Message Content intent, which is privileged.
	PrefixCommands bool

	// CoreURL is where the bot reaches the core API. Same process today, its
	// own container tomorrow; the bot does not know the difference.
	CoreURL string

	// APIAddr is where the core HTTP API listens. It carries no secrets and
	// has no auth, so the default binds inside the container only; publishing
	// it is a deliberate act, not a default.
	APIAddr string
}

// Load reads and validates the environment, failing at startup rather than
// letting the bot connect and misbehave.
func Load() (Config, error) {
	c := Config{
		DiscordToken:  strings.TrimSpace(os.Getenv("DISCORD_TOKEN")),
		DatabaseURL:   strings.TrimSpace(os.Getenv("DATABASE_URL")),
		Prefix:        strings.TrimSpace(os.Getenv("COMMAND_PREFIX")),
		LogLevel:      strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL"))),
		SourceBaseURL: strings.TrimSpace(os.Getenv("XOSO_BASE_URL")),
		GoldPrefix:    strings.TrimSpace(os.Getenv("GOLD_PREFIX")),
		GoldURL:       strings.TrimSpace(os.Getenv("GOLD_URL")),
		GuildID:       strings.TrimSpace(os.Getenv("DISCORD_GUILD_ID")),
		APIAddr:       strings.TrimSpace(os.Getenv("API_ADDR")),
		CoreURL:       strings.TrimSpace(os.Getenv("CORE_URL")),
	}
	if c.GoldPrefix == "" {
		c.GoldPrefix = "!gold"
	}
	if c.APIAddr == "" {
		c.APIAddr = ":8080"
	}
	if c.CoreURL == "" {
		c.CoreURL = "http://127.0.0.1" + c.APIAddr
	}

	var problems []string
	c.BackfillOnStart = boolEnv("BACKFILL_ON_START", true, &problems)
	c.BackfillDays = intEnv("BACKFILL_DAYS", 0, &problems)
	c.BackfillConcurrency = intEnv("BACKFILL_CONCURRENCY", 8, &problems)
	c.BackfillRate = durationEnv("BACKFILL_RATE", 0, &problems)
	c.GoldTTL = durationEnv("GOLD_TTL", 2*time.Minute, &problems)
	c.GoldGrace = durationEnv("GOLD_GRACE", 30*time.Minute, &problems)
	c.PrefixCommands = boolEnv("PREFIX_COMMANDS", true, &problems)
	if raw := strings.TrimSpace(os.Getenv("BACKFILL_FROM")); raw != "" {
		parsed, err := domain.ParseDate(raw, time.Now().In(domain.Location()))
		if err != nil {
			problems = append(problems, "BACKFILL_FROM: "+err.Error())
		} else {
			c.BackfillFrom = parsed
		}
	}
	if c.Prefix == "" {
		c.Prefix = "!xsmb"
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}

	if c.DiscordToken == "" {
		problems = append(problems, "DISCORD_TOKEN is empty")
	}
	if c.DatabaseURL == "" {
		problems = append(problems, "DATABASE_URL is empty")
	}
	if strings.ContainsAny(c.Prefix, " \t") {
		problems = append(problems, "COMMAND_PREFIX must not contain spaces")
	}
	if strings.ContainsAny(c.GoldPrefix, " \t") {
		problems = append(problems, "GOLD_PREFIX must not contain spaces")
	}
	if c.GoldPrefix != "" && c.GoldPrefix == c.Prefix {
		problems = append(problems, "GOLD_PREFIX and COMMAND_PREFIX must differ")
	}
	if len(problems) > 0 {
		return Config{}, fmt.Errorf("bad configuration: %s", strings.Join(problems, "; "))
	}
	return c, nil
}

// BackfillStart resolves the first day a background backfill reaches.
func (c Config) BackfillStart(now time.Time) time.Time {
	if !c.BackfillFrom.IsZero() {
		return c.BackfillFrom
	}
	if c.BackfillDays <= 0 {
		return domain.FirstDraw
	}
	return domain.DayOf(now).AddDate(0, 0, -c.BackfillDays)
}

func boolEnv(key string, fallback bool, problems *[]string) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		*problems = append(*problems, key+" must be true or false")
		return fallback
	}
	return value
}

func intEnv(key string, fallback int, problems *[]string) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		*problems = append(*problems, key+" must be a non-negative whole number")
		return fallback
	}
	return value
}

func durationEnv(key string, fallback time.Duration, problems *[]string) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value < 0 {
		*problems = append(*problems, key+` must be a duration such as "1500ms" or "2s"`)
		return fallback
	}
	return value
}
