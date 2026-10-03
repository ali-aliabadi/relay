// Package config parses Relay's environment-variable configuration.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ali-aliabadi/relay/internal/obs"
)

// EncryptionKeySize is the required length of RELAY_ENCRYPTION_KEY after base64 decoding.
const EncryptionKeySize = 32

// Config holds every setting from the ARCHITECTURE configuration table.
type Config struct {
	Addr               string
	DBPath             string
	TelegramBotToken   obs.Secret
	TelegramAPIURL     string
	WorkerPollInterval time.Duration
	LogLevel           slog.Level
	EncryptionKey      obs.SecretBytes
	RetentionDays      int
	MetricsAddr        string
	Pprof              bool
	MaxBodyBytes       int64
}

// LookupFunc matches os.LookupEnv so tests can inject an environment.
type LookupFunc func(key string) (string, bool)

// Load reads the configuration from lookup, applies defaults and validates it.
// All problems are reported together so a misconfigured deploy fails once, clearly.
func Load(lookup LookupFunc) (Config, error) {
	p := parser{lookup: lookup}
	cfg := Config{
		Addr:               p.str("RELAY_ADDR", ":8080"),
		DBPath:             p.str("RELAY_DB_PATH", "/data/relay.db"),
		TelegramBotToken:   obs.Secret(p.str("RELAY_TELEGRAM_BOT_TOKEN", "")),
		TelegramAPIURL:     p.str("RELAY_TELEGRAM_API_URL", "https://api.telegram.org"),
		WorkerPollInterval: p.duration("RELAY_WORKER_POLL_INTERVAL", time.Second),
		LogLevel:           p.level("RELAY_LOG_LEVEL", slog.LevelInfo),
		EncryptionKey:      p.key("RELAY_ENCRYPTION_KEY"),
		RetentionDays:      p.integer("RELAY_RETENTION_DAYS", 30),
		MetricsAddr:        p.str("RELAY_METRICS_ADDR", "127.0.0.1:9090"),
		Pprof:              p.boolean("RELAY_PPROF", false),
		MaxBodyBytes:       int64(p.integer("RELAY_MAX_BODY_BYTES", 7<<20)),
	}
	cfg.validate(&p)
	if len(p.errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %w", errors.Join(p.errs...))
	}
	return cfg, nil
}

func (c *Config) validate(p *parser) {
	if c.Addr == "" {
		p.fail("RELAY_ADDR", "must not be empty")
	}
	if c.DBPath == "" {
		p.fail("RELAY_DB_PATH", "must not be empty")
	}
	if u, err := url.Parse(c.TelegramAPIURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		p.fail("RELAY_TELEGRAM_API_URL", "must be an http(s) URL")
	}
	if c.WorkerPollInterval <= 0 {
		p.fail("RELAY_WORKER_POLL_INTERVAL", "must be positive")
	}
	if c.RetentionDays < 1 {
		p.fail("RELAY_RETENTION_DAYS", "must be at least 1")
	}
	if c.MaxBodyBytes < 1 {
		p.fail("RELAY_MAX_BODY_BYTES", "must be positive")
	}
	if c.MetricsAddr != "" && c.MetricsAddr == c.Addr {
		p.fail("RELAY_METRICS_ADDR", "must differ from RELAY_ADDR")
	}
}

// LogValue lists the settings without secrets, for a startup log line.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("addr", c.Addr),
		slog.String("db_path", c.DBPath),
		slog.Bool("telegram_enabled", c.TelegramBotToken != ""),
		slog.String("telegram_api_url", c.TelegramAPIURL),
		slog.Duration("worker_poll_interval", c.WorkerPollInterval),
		slog.String("log_level", c.LogLevel.String()),
		slog.Int("retention_days", c.RetentionDays),
		slog.String("metrics_addr", c.MetricsAddr),
		slog.Bool("pprof", c.Pprof),
		slog.Int64("max_body_bytes", c.MaxBodyBytes),
	)
}

type parser struct {
	lookup LookupFunc
	errs   []error
}

func (p *parser) fail(key, msg string) {
	p.errs = append(p.errs, fmt.Errorf("%s %s", key, msg))
}

func (p *parser) str(key, def string) string {
	v, ok := p.lookup(key)
	if !ok {
		return def
	}
	return strings.TrimSpace(v)
}

func (p *parser) duration(key string, def time.Duration) time.Duration {
	v := p.str(key, "")
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		p.fail(key, "must be a duration such as 1s or 500ms")
		return def
	}
	return d
}

func (p *parser) integer(key string, def int) int {
	v := p.str(key, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		p.fail(key, "must be an integer")
		return def
	}
	return n
}

func (p *parser) boolean(key string, def bool) bool {
	v := p.str(key, "")
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		p.fail(key, "must be true or false")
		return def
	}
	return b
}

func (p *parser) level(key string, def slog.Level) slog.Level {
	v := p.str(key, "")
	if v == "" {
		return def
	}
	var l slog.Level
	if err := l.UnmarshalText([]byte(v)); err != nil {
		p.fail(key, "must be one of debug, info, warn, error")
		return def
	}
	return l
}

// key decodes the required encryption key. Error messages never include the value.
func (p *parser) key(key string) obs.SecretBytes {
	v := p.str(key, "")
	if v == "" {
		p.fail(key, "is required (generate one with: openssl rand -base64 32)")
		return nil
	}
	b, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		p.fail(key, "must be standard base64")
		return nil
	}
	if len(b) != EncryptionKeySize {
		p.fail(key, fmt.Sprintf("must decode to %d bytes, got %d", EncryptionKeySize, len(b)))
		return nil
	}
	return b
}
