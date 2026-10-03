// Command faketelegram serves the fake Bot API (internal/channel/telegram/telegramtest)
// for the e2e suite. It runs in its own container next to the Relay image.
package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/ali-aliabadi/relay/internal/channel/telegram/telegramtest"
)

func main() {
	srv := &http.Server{
		Addr:              ":8081",
		Handler:           telegramtest.New(os.Getenv("FAKE_TELEGRAM_TOKEN")),
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("fake telegram stopped", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
