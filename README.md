# Relay

A personal notification gateway. Your apps and scripts send Relay a structured
message (who, how urgent, which template, what data), and Relay formats it,
picks the delivery channel, retries on failure and keeps a delivery log.

> **Status:** design phase. No code yet. See [docs/ROADMAP.md](docs/ROADMAP.md) for progress.

## Why

Every script, cron job and side project ends up with its own copy-pasted
Telegram bot code. Relay puts that in one place:

- **One API** for every app: `POST /v1/messages`.
- **Templates live in Relay**, so apps send data, not formatted text.
- **Urgency-aware routing**: `low` arrives silently, `critical` gets through.
- **Reliable delivery**: queued in SQLite, retried with backoff, every attempt logged.
- **Private by default**: message content encrypted at rest, purged after 30 days, never logged.
- **Pluggable channels**: Telegram today; SMS, email and push later.

## Example

```bash
curl -X POST https://relay.alialiabadi.ir/v1/messages \
  -H "Authorization: Bearer $RELAY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
        "to": ["ali"],
        "urgency": "high",
        "template": "backup.failed",
        "data": {"host": "nas", "error": "disk full"}
      }'
# → 202 {"id": "msg_01J...", "status": "queued"}
```

No template? Send text directly:

```json
{"to": ["ali"], "title": "Deploy done", "body": "v1.2 is live"}
```

## How it works

```
app → POST /v1/messages → stored as queued → worker → router (urgency) → renderer (template) → Telegram
```

Full design, data model and API: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Stack

Go · `net/http` · SQLite (`modernc.org/sqlite`) · sqlc · goose · slog + Prometheus · golangci-lint · testcontainers · Docker · Caddy · GitHub Actions

## Running locally

Once the skeleton lands (see roadmap):

```bash
cp .env.example .env              # set RELAY_TELEGRAM_BOT_TOKEN and RELAY_ENCRYPTION_KEY
make run                          # starts on :8080 with ./data/relay.db
go run ./cmd/relay clients create my-script     # prints an API key once
go run ./cmd/relay recipients add ali --name "Ali"
go run ./cmd/relay recipients link ali          # then send /start to the bot
```

Common commands: `make test`, `make lint`, `make build`, `make generate` (sqlc).

## Deployment

Pushing to `master` deploys: GitHub Actions builds the image, pushes it to
`ghcr.io/ali-aliabadi/relay`, SSHes into the VPS and runs `docker compose up -d`
in `/opt/relay`. The service is served at `https://relay.alialiabadi.ir` behind Caddy.

One-time setup is listed under "Phase 0" in [docs/ROADMAP.md](docs/ROADMAP.md).

## Configuration

All configuration is environment variables (`RELAY_ADDR`, `RELAY_DB_PATH`,
`RELAY_TELEGRAM_BOT_TOKEN`, ...). See the table in
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md#configuration).
