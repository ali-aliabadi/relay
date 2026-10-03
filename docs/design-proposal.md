# Relay: design proposal (draft for discussion)

Status: **draft, revision 2**. Decided: VPS + GitHub Actions deploy, Telegram-only MVP,
two hand-registered recipients. Pending: language (Go recommended, Python + uv the
alternative). Once settled, this becomes CLAUDE.md, README.md and docs/ROADMAP.md.

## One-line summary

Relay is a small self-hosted HTTP service. Your apps send it *what* happened
(template + data + urgency + who), and Relay decides *how* to say it and *where*
to send it (Telegram now; SMS, email, push later), retries on failure, and keeps a log.

## Guiding principles

- **Personal scale first.** One container, one SQLite file, no Redis/Kafka.
- **Callers never format.** Apps send structured data; templates and channel formatting live in Relay.
- **Accept fast, deliver async.** `POST` returns `202` immediately; delivery happens in a background worker with retries.
- **Channels are plugins.** Adding a channel (SMS, email) means implementing one small interface.
- **Everything is observable.** Every message and every delivery attempt is stored and queryable.

## Stack (Go, recommended)

| Concern | Choice | Why |
|---|---|---|
| Language | Go 1.23+ | Single static binary, low memory, strong stdlib |
| HTTP | `net/http` with 1.22+ pattern routing | No framework needed |
| Database | SQLite (WAL) via `modernc.org/sqlite` (pure Go, no cgo) | Zero ops, trivial cross-compile |
| Queries | `sqlc` (typed Go from SQL) | Plain SQL, compile-time checked |
| Migrations | `goose`, embedded in the binary, run on startup | No separate migration step in deploy |
| Queue | DB-backed outbox polled by an in-process worker goroutine | Durable across restarts, no extra infra |
| Templates | `text/template` | Per-channel variants; Telegram HTML escaping handled by the channel |
| Config | env vars | 12-factor, fits docker compose |
| Logging | `log/slog` (JSON) | Stdlib, structured |
| Tooling | `go test`, `golangci-lint`, `make` | One command each |

**Python alternative:** uv, FastAPI + Pydantic v2, SQLAlchemy 2 + Alembic, Jinja2, httpx, ruff, mypy, pytest. Same architecture and API.

## Architecture

```
 client app ──POST /v1/messages──▶ API ──validate + store (status=queued)──▶ SQLite
                                                                               │
                                     ┌──────────── worker (polls outbox) ◀─────┘
                                     ▼
                         Router: urgency + recipient prefs → ordered channel plan
                                     ▼
                         Renderer: template[channel] + data → channel-ready text
                                     ▼
                         Channel adapter (Telegram; later SMS, email...) → provider API
                                     ▼
                         DeliveryAttempt stored; retry w/ backoff or fall back to next channel
```

Code layout (Go):

```
cmd/relay/          main.go: `relay serve`, plus admin subcommands
internal/api/       handlers, auth middleware, request/response types
internal/core/      router (channel selection), renderer, worker, retry policy
internal/channel/   channel.go (interface), telegram/, fake/ (tests)
internal/store/     sqlc queries, generated code, migrations/
internal/config/    env parsing
deploy/             docker-compose.yml, Caddyfile
.github/workflows/  ci.yml (lint+test on PRs), deploy.yml (on push to master)
```

## Core concepts / data model

- **Client**: an app allowed to call Relay. `id, name, api_key_hash, created_at`.
- **Recipient**: a person. `id, username ("ali"), display_name, timezone, contacts {telegram_chat_id, ...}, channel_preference ["telegram"], quiet_hours`. Initially two people (ali and ali's wife), added by hand via the admin CLI.
- **Template**: `key ("backup.failed"), description, bodies {default, telegram, ...}`. Channel-specific body wins, else `default`.
- **Message**: one request. `id, client_id, recipients, urgency, template_key | (title, body), data (JSON), idempotency_key, status (queued|sending|delivered|partially_delivered|failed), created_at`.
- **DeliveryAttempt**: one try on one channel. `id, message_id, recipient_id, channel, status, provider_message_id, error, attempt_no, sent_at`.

### Getting a Telegram chat_id

Each recipient sends `/start` to the Relay bot once. The admin CLI (`relay recipients link ali`) reads the bot's pending updates and stores the chat_id. No inbound webhook needed for the MVP.

### Urgency → routing policy

| Urgency | Behaviour |
|---|---|
| `low` | Preferred channel, sent silently (Telegram `disable_notification`) |
| `normal` | Preferred channel, retry with backoff |
| `high` | Preferred channel; on failure fall back to the next channel (meaningful once a 2nd channel exists) |
| `critical` | All of the recipient's channels at once |

Callers can override with an explicit `channels: [...]` list. Quiet hours and digests come later.

## API (v1)

Auth: `Authorization: Bearer <api_key>` on every call except `/healthz`.

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/messages` | Send a message → `202 {id, status}` |
| `GET` | `/v1/messages/{id}` | Status plus delivery attempts |
| `GET` | `/v1/messages` | List/filter (status, client, since) |
| `GET/POST/PUT/DELETE` | `/v1/recipients[/{username}]` | Manage recipients |
| `GET/POST/PUT/DELETE` | `/v1/templates[/{key}]` | Manage templates |
| `POST` | `/v1/templates/{key}/preview` | Render a template for each channel without sending |
| `GET` | `/v1/channels` | Configured channels and their health |
| `GET` | `/healthz` | Liveness |

Example send:

```json
POST /v1/messages
{
  "to": ["ali"],
  "urgency": "high",
  "template": "backup.failed",
  "data": {"host": "nas", "error": "disk full"},
  "idempotency_key": "backup-2026-10-03"
}
```

Or without a template: `{"to": ["ali"], "title": "Deploy done", "body": "v1.2 is live"}`.

## Deployment

- VPS (`german-vps`) already has Docker. Relay runs via `docker compose` with a named volume for the SQLite file.
- Public URL: `https://relay.alialiabadi.ir`, behind **Caddy** (automatic HTTPS). If the VPS already has a reverse proxy, plug into that instead.
- **CI** (`ci.yml`): on every PR and push, run lint + tests + build.
- **CD** (`deploy.yml`): on push to `master`, build the image, push to GHCR (`ghcr.io/ali-aliabadi/relay`), SSH to the VPS, `docker compose pull && docker compose up -d`, then hit `/healthz`.
- GitHub secrets needed: `VPS_HOST`, `VPS_USER`, `VPS_SSH_KEY` (a dedicated deploy key), optionally `VPS_PORT`. Runtime secrets (`TELEGRAM_BOT_TOKEN`, etc.) live in an `.env` file on the VPS, never in the repo.
- Backups: nightly copy of the SQLite file (later item).

## MVP (v0.1)

1. Project skeleton, Makefile, lint + test, Dockerfile, CI workflow
2. SQLite store + migrations
3. API-key auth (admin CLI creates keys; only hashes stored)
4. Recipients (hand-registered) and templates CRUD, template preview
5. `POST /v1/messages` with validation and idempotency
6. Background worker: outbox polling, retry with exponential backoff
7. Urgency router
8. Telegram channel + fake channel for tests; `relay recipients link` for chat_id
9. Message status + delivery attempts endpoints
10. Deploy: docker compose + Caddy on the VPS, GitHub Actions deploy on push to `master`

## Later

- **SMS channel** (provider to be decided) and real fallback for `high`
- Quiet hours and low-urgency digests
- Scheduled / delayed sends (`send_at`)
- More channels: email (SMTP), ntfy/push, Discord
- Telegram inbound: ack buttons; unacked `critical` messages escalate
- Self-registration of recipients by username (e.g. via the Telegram bot)
- Delivery status webhooks back to the calling app
- Rate limiting and per-client quotas
- Small web dashboard for the message log
- Client library and a `relay send` CLI for scripts
- MCP server so Claude/agents can notify you through Relay
- SQLite backups to off-site storage
