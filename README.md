# Relay

A personal notification gateway. Your apps and scripts send Relay a structured
message (who, how urgent, and content like text, a table or an image), and Relay
formats it for the platform,
picks the delivery channel, retries on failure and keeps a delivery log.

> **Status:** early development. The service skeleton (config, logging, `/healthz`, tooling, CI) is in; messaging is not yet. See [docs/ROADMAP.md](docs/ROADMAP.md) for progress.

## Why

Every script, cron job and side project ends up with its own copy-pasted
Telegram bot code. Relay puts that in one place:

- **One API** for every app: `POST /v1/messages`.
- **No templates to manage**: apps send content blocks; each platform has one built-in layout.
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
        "title": "Backup failed",
        "blocks": [
          {"type": "text", "text": "Nightly backup of nas stopped."},
          {"type": "table", "columns": ["Disk", "Used"], "rows": [["sda", "98%"]]}
        ]
      }'
# → 202 {"id": "msg_01J...", "status": "queued"}
```

Block types: `text`, `fields`, `table`, `image`, `code`, `link`, `question`. For a plain message:

```json
{"to": ["ali"], "text": "Deploy done: v1.2 is live"}
```

### Asking a question

Add a `question` block. With `options` the recipient gets buttons; without, they
reply to the message in Telegram. `webhook` (optional, public `https` only) is
called with `{"event": "answer", "message_id": "msg_..."}` when an answer arrives.

```json
{"to": ["ali"], "blocks": [
  {"type": "text", "text": "v2 passed staging."},
  {"type": "question", "text": "Ship v2 to prod?", "options": ["Yes", "No"],
   "webhook": "https://my-app.example/relay-hook"}
]}
```

Then poll (or fetch on the webhook) `GET /v1/messages/{id}/answers`:

```json
{"answers": [{"recipient": "ali", "answer": "Yes", "answered_at": "2026-10-04T09:12:44.123Z"}]}
```

Each recipient answers separately and can change their answer until the app
fetches it; after that it's final.

## Using Relay from your apps' AI agents

[`skills/relay-notify/SKILL.md`](skills/relay-notify/SKILL.md) teaches an agent
the whole API: who to send to (`admin` when unsure), urgency, blocks, asking
questions and reading answers, retries and privacy. Copy the folder into the
other app's `.claude/skills/` and give that app `RELAY_URL` and its own
`RELAY_API_KEY` (`relay clients create <app-name>`).

## How it works

```
app → POST /v1/messages → stored as queued → worker → router (urgency) → Telegram layout → Telegram
```

Full design, data model and API: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Stack

Go · `net/http` · SQLite (`modernc.org/sqlite`) · sqlc · goose · slog + Prometheus · golangci-lint · testcontainers · Docker · nginx · GitHub Actions

## Running locally

Needs Go (the toolchain in `go.mod` is fetched automatically) and, for e2e tests, Docker.

```bash
make tools                        # installs pinned golangci-lint, gofumpt, govulncheck, ... into bin/tools
cp .env.example .env              # set RELAY_ENCRYPTION_KEY (openssl rand -base64 32)
make run                          # starts on :8080 with ./data/relay.db
curl localhost:8080/healthz       # {"status":"ok"}
```

Then register a recipient and send yourself a message (needs `RELAY_TELEGRAM_BOT_TOKEN`):

```bash
go run ./cmd/relay clients create my-script     # prints an API key once
go run ./cmd/relay recipients add ali --name "Ali"
go run ./cmd/relay recipients alias ali admin   # optional: more names for the same person
go run ./cmd/relay recipients link ali @your_telegram_username
# with `make run` going in another terminal: open the bot and tap Start; it confirms the link
# (no Telegram username? /start replies with a code: recipients link ali <code>)
go run ./cmd/relay send --to ali "hello"
```

Common commands: `make check` (everything CI runs), `make test`, `make test-e2e`, `make lint`, `make build`, `make help` for the rest.

## Deployment

Merging to `master` deploys once CI passes: GitHub Actions builds the image,
pushes it to `ghcr.io/ali-aliabadi/relay`, copies `deploy/` to `/opt/relay` on the
VPS over SSH and runs `docker compose up -d` there. The service is served at
`https://relay.alialiabadi.ir` through the VPS's existing nginx (site config in
`deploy/nginx-relay.conf`, installed once by hand). On the VPS, admin commands run inside
the container:

```bash
cd /opt/relay && docker compose exec relay relay clients create my-app
```

One-time setup is listed under "Phase 0" in [docs/ROADMAP.md](docs/ROADMAP.md).

## Configuration

All configuration is environment variables (`RELAY_ADDR`, `RELAY_DB_PATH`,
`RELAY_TELEGRAM_BOT_TOKEN`, ...). See the table in
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md#configuration).
