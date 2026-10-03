package main

import (
	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/channel/telegram"
	"github.com/ali-aliabadi/relay/internal/config"
)

// buildChannels returns the channels enabled by configuration.
func buildChannels(cfg config.Config) []channel.Channel {
	var out []channel.Channel
	if cfg.TelegramBotToken != "" {
		out = append(out, telegram.New(telegram.NewClient(cfg.TelegramAPIURL, cfg.TelegramBotToken, nil)))
	}
	return out
}
