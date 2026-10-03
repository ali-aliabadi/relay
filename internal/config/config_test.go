package config

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// testKey is a fixed, obviously fake 32-byte key.
var testKey = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", EncryptionKeySize)))

func env(kv map[string]string) LookupFunc {
	return func(k string) (string, bool) {
		v, ok := kv[k]
		return v, ok
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{"RELAY_ENCRYPTION_KEY": testKey}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	checks := []struct {
		name      string
		got, want any
	}{
		{"Addr", cfg.Addr, ":8080"},
		{"DBPath", cfg.DBPath, "/data/relay.db"},
		{"TelegramBotToken", cfg.TelegramBotToken.Reveal(), ""},
		{"WorkerPollInterval", cfg.WorkerPollInterval, time.Second},
		{"LogLevel", cfg.LogLevel, slog.LevelInfo},
		{"RetentionDays", cfg.RetentionDays, 30},
		{"MetricsAddr", cfg.MetricsAddr, "127.0.0.1:9090"},
		{"Pprof", cfg.Pprof, false},
		{"MaxBodyBytes", cfg.MaxBodyBytes, int64(7340032)},
		{"len(EncryptionKey)", len(cfg.EncryptionKey), EncryptionKeySize},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"RELAY_ENCRYPTION_KEY":       testKey,
		"RELAY_ADDR":                 "127.0.0.1:9999",
		"RELAY_DB_PATH":              "./data/relay.db",
		"RELAY_TELEGRAM_BOT_TOKEN":   " fake-token ",
		"RELAY_WORKER_POLL_INTERVAL": "250ms",
		"RELAY_LOG_LEVEL":            "debug",
		"RELAY_RETENTION_DAYS":       "7",
		"RELAY_METRICS_ADDR":         "",
		"RELAY_PPROF":                "true",
		"RELAY_MAX_BODY_BYTES":       "1024",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != "127.0.0.1:9999" || cfg.DBPath != "./data/relay.db" ||
		cfg.TelegramBotToken.Reveal() != "fake-token" || cfg.WorkerPollInterval != 250*time.Millisecond ||
		cfg.LogLevel != slog.LevelDebug || cfg.RetentionDays != 7 || cfg.MetricsAddr != "" ||
		!cfg.Pprof || cfg.MaxBodyBytes != 1024 {
		t.Errorf("overrides not applied: %+v", cfg)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"missing key", map[string]string{}, "RELAY_ENCRYPTION_KEY is required"},
		{"key not base64", map[string]string{"RELAY_ENCRYPTION_KEY": "not base64!"}, "must be standard base64"},
		{"key too short", map[string]string{"RELAY_ENCRYPTION_KEY": base64.StdEncoding.EncodeToString([]byte("short"))}, "got 5"},
		{"bad duration", map[string]string{"RELAY_WORKER_POLL_INTERVAL": "soon"}, "RELAY_WORKER_POLL_INTERVAL must be a duration"},
		{"zero duration", map[string]string{"RELAY_WORKER_POLL_INTERVAL": "0s"}, "RELAY_WORKER_POLL_INTERVAL must be positive"},
		{"bad level", map[string]string{"RELAY_LOG_LEVEL": "loud"}, "RELAY_LOG_LEVEL must be one of"},
		{"bad retention", map[string]string{"RELAY_RETENTION_DAYS": "x"}, "RELAY_RETENTION_DAYS must be an integer"},
		{"zero retention", map[string]string{"RELAY_RETENTION_DAYS": "0"}, "RELAY_RETENTION_DAYS must be at least 1"},
		{"bad bool", map[string]string{"RELAY_PPROF": "maybe"}, "RELAY_PPROF must be true or false"},
		{"zero body", map[string]string{"RELAY_MAX_BODY_BYTES": "0"}, "RELAY_MAX_BODY_BYTES must be positive"},
		{"empty addr", map[string]string{"RELAY_ADDR": ""}, "RELAY_ADDR must not be empty"},
		{"empty db", map[string]string{"RELAY_DB_PATH": " "}, "RELAY_DB_PATH must not be empty"},
		{"same addrs", map[string]string{"RELAY_ADDR": ":1", "RELAY_METRICS_ADDR": ":1"}, "must differ"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := tt.env["RELAY_ENCRYPTION_KEY"]; !ok && tt.name != "missing key" {
				tt.env["RELAY_ENCRYPTION_KEY"] = testKey
			}
			_, err := Load(env(tt.env))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadReportsAllErrors(t *testing.T) {
	_, err := Load(env(map[string]string{"RELAY_LOG_LEVEL": "loud", "RELAY_PPROF": "maybe"}))
	for _, want := range []string{"RELAY_ENCRYPTION_KEY", "RELAY_LOG_LEVEL", "RELAY_PPROF"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error %v does not mention %s", err, want)
		}
	}
}

func TestSecretsNeverPrinted(t *testing.T) {
	const token = "123456:fake-bot-token"
	cfg, err := Load(env(map[string]string{"RELAY_ENCRYPTION_KEY": testKey, "RELAY_TELEGRAM_BOT_TOKEN": token}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var sb strings.Builder
	slog.New(slog.NewJSONHandler(&sb, nil)).Info("cfg", slog.Any("config", cfg))
	outputs := []string{sb.String(), fmt.Sprintf("%v %+v %#v", cfg, cfg, cfg)}
	for _, out := range outputs {
		if strings.Contains(out, token) || strings.Contains(out, testKey) || strings.Contains(out, "kkkk") {
			t.Errorf("secret leaked in %q", out)
		}
	}
	if !strings.Contains(sb.String(), `"telegram_enabled":true`) {
		t.Errorf("log value missing telegram_enabled: %s", sb.String())
	}
}

func TestBadKeyErrorDoesNotEchoValue(t *testing.T) {
	const bad = "c2VjcmV0LXZhbHVl" // valid base64, wrong length
	_, err := Load(env(map[string]string{"RELAY_ENCRYPTION_KEY": bad}))
	if err == nil || strings.Contains(err.Error(), bad) {
		t.Fatalf("error %v echoes the key", err)
	}
}
