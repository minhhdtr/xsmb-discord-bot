package config_test

import (
	"strings"
	"testing"

	"github.com/minhhdtr/xsmb-discord-bot/internal/config"
)

func setEnv(t *testing.T, values map[string]string) {
	t.Helper()
	for _, key := range []string{"DISCORD_TOKEN", "DATABASE_URL", "COMMAND_PREFIX", "LOG_LEVEL"} {
		t.Setenv(key, values[key])
	}
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, map[string]string{"DISCORD_TOKEN": "tok", "DATABASE_URL": "postgres://x"})
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Prefix != "!xsmb" || cfg.LogLevel != "info" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadTrimsAndOverrides(t *testing.T) {
	setEnv(t, map[string]string{
		"DISCORD_TOKEN": "  tok  ", "DATABASE_URL": " postgres://x ",
		"COMMAND_PREFIX": " !kq ", "LOG_LEVEL": " DEBUG ",
	})
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DiscordToken != "tok" || cfg.Prefix != "!kq" || cfg.LogLevel != "debug" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadReportsEveryProblemAtOnce(t *testing.T) {
	setEnv(t, map[string]string{"COMMAND_PREFIX": "! xsmb"})
	_, err := config.Load()
	if err == nil {
		t.Fatal("accepted an empty configuration")
	}
	for _, want := range []string{"DISCORD_TOKEN", "DATABASE_URL", "COMMAND_PREFIX"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %s", err, want)
		}
	}
}
