package config_test

import (
	"strings"
	"testing"

	"github.com/minhhdtr/xsmb-discord-bot/internal/config"
)

func setEnv(t *testing.T, values map[string]string) {
	t.Helper()
	// Every variable the service reads, cleared unless the case sets it, so a
	// value left over from the developer's shell cannot make a test pass.
	for _, key := range []string{
		"DATABASE_URL", "LOG_LEVEL", "API_ADDR", "XOSO_BASE_URL", "GOLD_URL",
		"BACKFILL_ON_START", "BACKFILL_DAYS", "BACKFILL_CONCURRENCY",
	} {
		t.Setenv(key, values[key])
	}
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, map[string]string{"DATABASE_URL": "postgres://x"})
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", cfg.LogLevel)
	}
	// Bound to every interface inside the container, never published to the
	// host: the API has no auth and five of its endpoints write.
	if cfg.APIAddr != ":8080" {
		t.Errorf("APIAddr = %q, want :8080", cfg.APIAddr)
	}
}

func TestLoadTrimsAndOverrides(t *testing.T) {
	setEnv(t, map[string]string{
		"DATABASE_URL": " postgres://x ", "LOG_LEVEL": " DEBUG ", "API_ADDR": " :9000 ",
	})
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseURL != "postgres://x" || cfg.LogLevel != "debug" || cfg.APIAddr != ":9000" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadReportsEveryProblemAtOnce(t *testing.T) {
	setEnv(t, map[string]string{"BACKFILL_CONCURRENCY": "nhiều"})
	_, err := config.Load()
	if err == nil {
		t.Fatal("accepted an empty configuration")
	}
	// Both problems in one message: fixing configuration one error per restart
	// is miserable.
	for _, want := range []string{"DATABASE_URL", "BACKFILL_CONCURRENCY"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %s", err, want)
		}
	}
}

// Nothing here reads a Discord variable any more. If one comes back, it means
// chat has leaked into the service that owns the database.
func TestNoDiscordConfigurationRemains(t *testing.T) {
	setEnv(t, map[string]string{"DATABASE_URL": "postgres://x"})
	t.Setenv("DISCORD_TOKEN", "should-be-ignored")

	if _, err := config.Load(); err != nil {
		t.Fatalf("a stray DISCORD_TOKEN broke the load: %v", err)
	}
}
