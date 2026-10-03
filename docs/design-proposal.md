# Relay: design proposal (draft for discussion)

Status: **draft**. Once the open questions at the bottom are settled, this becomes
CLAUDE.md, README.md and docs/ROADMAP.md.

## One-line summary

Relay is a small self-hosted HTTP service. Your apps send it *what* happened
(template + data + urgency + who), and Relay decides *how* to say it and *where*
to send it (Telegram, SMS, later email/push), retries on failure, and keeps a log.

## Guiding principles

- **Personal scale first.** One container, one SQLite file, no Redis/Kafka. Thousands of messages a day, not millions.
- **Callers never format.** Apps send structured data; templates and channel formatting live in Relay.
- **Accept fast, deliver async.** `POST` returns `202` immediately; delivery happens in a background worker with retries.
- **Channels are plugins.** Adding a channel means implementing one small interface, nothing else changes.
- **Everything is observable.** Every message and every delivery attempt is stored and queryable.

## Recommended stack

| Concern | Choice | Why |
|---|---|---|
| Language | Python 3.12+ | Fast to build, great libraries, easy for agents to work in |
| Web framework | FastAPI + Pydantic v2 | Typed request validation, free OpenAPI docs at `/docs` |
| Database | SQLite (WAL mode) via SQLAlchemy 2 + Alembic | Zero ops; can switch to Postgres later without code changes |
| Queue | DB-backed outbox polled by an in-process async worker | Durable across restarts without extra infrastructure |
| Templates | Jinja2 (sandboxed) | Familiar, supports per-channel variants |
| HTTP client | httpx (async) | For Telegram Bot API and SMS provider APIs |
| Tooling | uv, ruff, mypy, pytest | One command each; fast in CI and locally |
| Packaging | Single Docker image + docker-compose | Runs the same on a VPS, home server or laptop |

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
                         Channel adapter (Telegram | SMS | ...) → provider API
                                     ▼
                         DeliveryAttempt stored; retry w/ backoff or fall back to next channel
```

Code layout:

```
src/relay/
  api/          FastAPI routers, auth dependency, request/response schemas
  core/         router (channel selection), renderer, dispatcher/worker, retry policy
  channels/     base.py (Channel protocol), telegram.py, sms_<provider>.py, fake.py (tests)
  db/           SQLAlchemy models, session, Alembic migrations
  config.py     pydantic-settings, all config from env vars
  cli.py        admin CLI: create API key, add recipient, send test message
tests/
```

## Core concepts / data model

- **Client**: an app allowed to call Relay. `id, name, api_key_hash, created_at`.
- **Recipient**: a person (mostly you). `id, handle ("ali"), name, timezone, contacts {telegram_chat_id, phone, ...}, channel_preference ["telegram","sms"], quiet_hours`.
- **Template**: `key ("backup.failed"), description, bodies {default, telegram, sms}`. Channel-specific body wins, else `default`.
- **Message**: one request. `id, client_id, recipients, urgency, template_key | (title, body), data (JSON), idempotency_key, status (queued|sending|delivered|partially_delivered|failed), created_at`.
- **DeliveryAttempt**: one try on one channel. `id, message_id, recipient_id, channel, status, provider_message_id, error, attempt_no, sent_at`.

### Urgency → routing policy

| Urgency | Behaviour |
|---|---|
| `low` | Preferred channel only, held during quiet hours (later: batched into a digest) |
| `normal` | Preferred channel, retry with backoff |
| `high` | Preferred channel; if it fails, fall back to the next channel (e.g. Telegram → SMS) |
| `critical` | All of the recipient's channels at once, ignores quiet hours |

Callers can override with an explicit `channels: [...]` list.

## API (v1)

Auth: `Authorization: Bearer <api_key>` on every call except `/healthz`.

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/messages` | Send a message → `202 {id, status}` |
| `GET` | `/v1/messages/{id}` | Status plus delivery attempts |
| `GET` | `/v1/messages` | List/filter (status, client, since) |
| `GET/POST/PUT/DELETE` | `/v1/recipients[/{handle}]` | Manage recipients |
| `GET/POST/PUT/DELETE` | `/v1/templates[/{key}]` | Manage templates |
| `POST` | `/v1/templates/{key}/preview` | Render a template for each channel without sending |
| `GET` | `/v1/channels` | Configured channels and whether they are healthy |
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

## MVP (v0.1)

1. Project skeleton: uv, FastAPI app, config via env, ruff/mypy/pytest, Dockerfile, CI on GitHub Actions
2. SQLite models + Alembic migrations
3. API-key auth (CLI to create keys; only hashes stored)
4. Recipients and templates CRUD, template preview
5. `POST /v1/messages` with validation and idempotency
6. Background worker: outbox polling, retry with exponential backoff
7. Urgency router with fallback
8. Channels: Telegram (Bot API) and one SMS provider, plus a fake channel for tests
9. Message status + delivery attempts endpoints
10. docker-compose deploy instructions

## Later

- Quiet hours enforcement and low-urgency digests
- Scheduled / delayed sends (`send_at`)
- More channels: email (SMTP), ntfy/push, Discord, Slack
- Telegram inbound: acknowledge buttons, so `critical` messages escalate to SMS if not acked within N minutes
- Delivery status webhooks back to the calling app
- Rate limiting and per-client quotas
- Small web dashboard for the message log
- Python client SDK and a tiny CLI (`relay send ...`)
- MCP server so Claude/agents can notify you through Relay
- Postgres support for larger deployments

## Open questions (these change the design)

1. **Language**: Python + FastAPI (recommended), TypeScript (Node/Fastify), or Go?
2. **Hosting**: VPS with Docker (recommended), a home server, or something serverless?
3. **SMS provider**: which country are your numbers in? Twilio is the default for most countries; a local provider (e.g. Kavenegar for Iran) may be needed elsewhere.
4. **Recipients**: just you, or also family/friends/other people? Just you keeps auth and the data model simpler.
