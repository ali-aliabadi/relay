# Relay architecture

This is the design reference for Relay. CLAUDE.md summarizes it for agents;
docs/ROADMAP.md tracks what is built. When the design changes, update this file
in the same PR.

## What Relay is

A small self-hosted HTTP service. Apps send it a structured message (who, how
urgent, and content blocks such as text, a table or an image). Relay decides *how*
it looks on each platform and *where* to send it (Telegram
first; SMS, email and push later), retries on failure, and keeps a log of every
message and delivery attempt.

## Principles

- **Personal scale.** One container, one SQLite file, no Redis/Kafka/queues.
- **Callers never format.** Apps send content blocks; each channel has one built-in layout that turns blocks into that platform's format.
- **No per-message templates.** Adding a new app or a new kind of notification needs no setup in Relay. Layouts are code, one per channel (a second only if a real need appears).
- **Accept fast, deliver async.** `POST /v1/messages` stores the message and returns `202`; a background worker delivers it.
- **Answers are pulled, not pushed.** A `question` block collects replies; apps fetch them with `GET /v1/messages/{id}/answers` and can get a content-free webhook nudge.
- **Channels are plugins.** A channel is one Go package implementing one interface. Nothing else changes when a channel is added.
- **Everything is observable.** Every message and every delivery attempt is stored and queryable through the API.
- **Few dependencies.** Prefer the standard library. Each new dependency needs a reason.

## Scope

- Users: ali and ali's wife, registered by hand via the admin CLI.
- MVP channel: Telegram only. SMS comes later; provider not chosen yet.
- Hosting: existing VPS (`german-vps`) with Docker, behind the VPS's existing nginx at `relay.alialiabadi.ir`.
- Deploy: merging to `master` deploys via GitHub Actions.

## Stack

| Concern | Choice |
|---|---|
| Language | Go (latest stable; version pinned in `go.mod`) |
| HTTP | `net/http` with Go 1.22+ method/pattern routing, no framework |
| Database driver | `modernc.org/sqlite` (pure Go, no cgo, so static builds and easy cross-compile) |
| Queries | `sqlc` generates typed Go from SQL files in `internal/store/queries/` |
| Migrations | `pressly/goose`, SQL files embedded with `embed`, applied on startup |
| IDs | ULIDs with a type prefix: `msg_01J...`, `rcp_...`, `cli_...` (`oklog/ulid`) |
| Formatting | Per-channel layout code; Telegram output built with `html.EscapeString` on every value into Telegram's HTML subset |
| Config | Environment variables only |
| Logging | `log/slog`, JSON output, redaction helpers |
| Metrics | `prometheus/client_golang` on a separate internal listener |
| Encryption | AES-256-GCM (stdlib `crypto/aes`, `crypto/cipher`) for private columns |
| Lint / format | `golangci-lint` (v2 config), `gofumpt`, `goimports`, file-length script |
| Tests | `go test -race`, `testcontainers-go` for e2e, fuzzing, golden files |
| Security tooling | `gosec` (via golangci-lint), `govulncheck`, `gitleaks`, `trivy`, Dependabot |

## Components

```
 client app ──POST /v1/messages──▶ api ──validate, store (status=queued)──▶ SQLite
                                                                             │
                                 ┌──────────── worker (polls outbox) ◀───────┘
                                 ▼
                     router: urgency + recipient prefs → ordered channel plan
                                 ▼
                     layout: content blocks → channel's format (text, photo, buttons)
                                 ▼
                     channel adapter (telegram) → provider API
                                 ▼
             delivery_attempt stored; retry with backoff, or fall back to next channel
```

- **api** (`internal/api`): HTTP handlers, bearer-token auth middleware, JSON request/response types, validation. Handlers never call channels directly.
- **store** (`internal/store`): migrations, sqlc queries, transactions. The only package that touches SQL.
- **worker** (`internal/core/worker.go`): a goroutine that every `RELAY_WORKER_POLL_INTERVAL` claims due deliveries (`status=queued AND next_attempt_at <= now`), sends them, and records the result. Claiming is a single `UPDATE ... RETURNING` so a restart never double-sends a claimed row; rows stuck in `sending` longer than a timeout are requeued on startup.
- **router** (`internal/core/router.go`): turns (message, recipient) into an ordered list of channels using urgency, the recipient's `channel_preference`, and any explicit `channels` override.
- **message** (`internal/message`): the content block types, their validation and limits. Shared by the API and every layout.
- **layouts**: each channel package owns its one layout (`internal/channel/telegram/layout.go`), turning blocks into what that platform supports and enforcing its limits (Telegram: 4096 chars per text, 1024 per photo caption; truncate with `…`).
- **channels** (`internal/channel`): the `Channel` interface and one package per provider. `fake` records sends in memory for tests.
- **answers** (`internal/core/answers.go`): matches a reply to its delivery (only from the chat it was sent to), checks it against the question, stores it and fires the webhook (`internal/core/webhooks.go`).
- **telegram poller** (`internal/channel/telegram/poller.go`): long-polls `getUpdates` inside `serve` for button taps, replies and `/start`. Telegram allows one reader per bot, so nothing else polls.
- **cli** (`cmd/relay`): one binary with subcommands: `serve`, `migrate`, `clients`, `recipients`, `send`, `version`.

### Channel interface

```go
type Channel interface {
    Name() string                                  // "telegram"
    Send(ctx context.Context, to Contact, msg message.Message) (providerID string, err error)
    Preview(msg message.Message) (Preview, error)   // what Send would produce, no network
}
```

`Send` returns a `*channel.Error` with `Permanent bool` and optional `RetryAfter`.
Permanent errors (bad chat id, bot blocked) fail the attempt immediately;
transient errors (timeouts, 5xx, 429) are retried.

## Data model

```
clients          id, name (unique), api_key_hash (sha256, unique), created_at, revoked_at
recipients       id, username (unique), display_name, timezone,
                 channel_preference (JSON array), created_at
contacts         recipient_id, channel, address (e.g. telegram chat_id), verified_at
messages         id, client_id, urgency, title NULL, blocks (JSON) NULL, source NULL,
                 idempotency_key NULL, request_id NULL, status, created_at, redacted_at NULL
                 UNIQUE (client_id, idempotency_key)
attachments      id, message_id, content_type, bytes (BLOB), size  -- inline images
deliveries       id, message_id, recipient_id, channel, status
                 (queued|sending|delivered|failed), attempts, next_attempt_at,
                 provider_message_id, last_error, created_at, updated_at
answers          message_id, recipient_id, answer, answered_at, fetched_at NULL
                 PRIMARY KEY (message_id, recipient_id)
invites          recipient_id, channel, handle (Telegram username), created_at
                 PRIMARY KEY (recipient_id, channel)
aliases          name (PK), recipient_id, created_at
```

Tables are SQLite `STRICT`. Times are stored as UTC text in a fixed-width format
(`2006-01-02T15:04:05.000Z`) so they sort correctly as text. Deleting a recipient
cascades to its contacts and deliveries.

Private columns (`messages.title`, `messages.blocks`, `attachments.bytes`,
`contacts.address`, `answers.answer`, `invites.handle`) are stored encrypted (see Privacy and security).

An answer can be replaced while `fetched_at` is NULL. The first
`GET /v1/messages/{id}/answers` that returns it sets `fetched_at`, and from then
on it is final. `provider_message_id` is the Telegram ID of the last part sent
(the one with the question and buttons), which typed replies point at.

A message fans out into one `deliveries` row per recipient per planned channel.
Message status is derived from its deliveries: `queued`, `sending`, `delivered`
(all delivered), `partially_delivered`, or `failed`.

Retry policy: up to 5 attempts with backoff 10s, 30s, 2m, 10m, 30m, honouring a
provider's `RetryAfter` when given. After the last failure the delivery is
`failed`. For `high` urgency the next channel in the plan is queued as soon as
the preferred channel fails twice (or fails permanently), so fallback is fast.

## Urgency

| Urgency | MVP behaviour (Telegram only) | With more channels |
|---|---|---|
| `low` | Sent silently (`disable_notification`) | Held during quiet hours, later batched into a digest |
| `normal` | Sent normally | Preferred channel only |
| `high` | Sent normally | Preferred channel, fall back to the next on failure |
| `critical` | Sent normally, prefixed with 🚨 | All channels at once, ignores quiet hours |

Callers may pass `channels: ["telegram"]` to override routing.

## API (v1)

All endpoints except `/healthz` require `Authorization: Bearer <api_key>`.
Errors use one shape: `{"error": {"code": "invalid_request", "message": "..."}}`.

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/messages` | Queue a message → `202 {"id", "status"}`; repeat with the same `idempotency_key` → `200` with the original |
| `GET` | `/v1/messages/{id}` | Message status plus its deliveries |
| `GET` | `/v1/messages` | List, filter by `status`, `since`, `limit`, cursor pagination |
| `GET` | `/v1/messages/{id}/answers` | Answers to the message's question so far; returning one makes it final |
| `GET` `POST` | `/v1/recipients` | List / create recipients |
| `GET` `PUT` `DELETE` | `/v1/recipients/{username}` | Read / update / delete a recipient |
| `POST` | `/v1/preview` | Same body as `/v1/messages`; returns what each channel would send, no delivery |
| `GET` | `/v1/channels` | Configured channels and their health |
| `GET` | `/healthz` | Liveness, checks the DB |

### Request and response details

- Validation failures are `422` with `{"error": {"code": "invalid_request", "message": "...", "problems": ["to[1]: unknown recipient", ...]}}`. Problems name field paths and rules, never the caller's values (not even usernames).
- Malformed JSON, unknown fields or trailing data are `400 invalid_json`; a body over `RELAY_MAX_BODY_BYTES` is `413 too_large`.
- A recipient must have a linked contact on a configured channel, otherwise `to[i]: recipient has no linked channel to send on`.
- Every name in `to` is a username or an **alias** (another name for a recipient, e.g. `admin` → `ali`, managed with `relay recipients alias|unalias`). Names that reach the same person collapse into one delivery, so `["admin", "ali"]` sends once. Aliases and usernames share one namespace, so a name always means exactly one person. Answers report the real username.
- `GET /v1/messages/{id}/answers` returns `{"answers": [{"recipient": "<username>", "answer", "answered_at"}]}`, one per recipient who answered (empty while waiting). It is the only endpoint that returns content, because the asking app is its source. Another client's message is `404`; answers are deleted with the content at retention.
- `GET /v1/messages/{id}` returns metadata only (`id`, `status`, `urgency`, `source`, `request_id`, `created_at`, `redacted`) plus `deliveries` (`id`, `recipient_id`, `channel`, `status`, `attempts`, `next_attempt_at`, `last_error`, `updated_at`). Content is never returned. Another client's message is `404`.
- `GET /v1/messages?status=&since=<RFC 3339>&limit=1-100&cursor=` returns `{"messages": [...], "next_cursor": "msg_..."}`, newest first; `next_cursor` is present when the page is full.
- Recipients: `POST` takes `username`, `display_name`, optional `timezone` (IANA, default `UTC`) and `channel_preference` (default `["telegram"]`); `PUT` replaces `display_name`, `timezone` and `channel_preference`. Responses add `aliases`, `linked_channels` (channel names only, never addresses) and `created_at`. A username already taken by an alias is `409`. Recipients are addressed by username in these routes; aliases are for `to`.
- Inline images are moved out of the blocks into `attachments`; the stored image block keeps an `attachment` index instead of the base64.

### Message format

A message is an optional `title` plus a list of content `blocks`. Callers never
send markup; every value is plain text and the channel's layout formats it.

```json
POST /v1/messages
{
  "to": ["ali"],
  "urgency": "high",
  "source": "backup-script",
  "title": "Backup failed",
  "blocks": [
    {"type": "text",   "text": "Nightly backup of nas stopped."},
    {"type": "fields", "items": [{"label": "Host", "value": "nas"}, {"label": "Error", "value": "disk full"}]},
    {"type": "table",  "columns": ["Disk", "Used"], "rows": [["sda", "98%"], ["sdb", "41%"]]},
    {"type": "image",  "url": "https://grafana.example/render/disk.png", "caption": "Disk usage"},
    {"type": "code",   "text": "rsync: write failed: No space left on device"},
    {"type": "link",   "text": "Open dashboard", "url": "https://grafana.example/d/disks"},
    {"type": "question", "text": "Retry now?", "options": ["Yes", "No"], "webhook": "https://app.example/hook"}
  ],
  "idempotency_key": "backup-nas-2026-10-03"
}
```

Shorthand for the common case: `{"to": ["ali"], "text": "Deploy done: v1.2 is live"}`
is the same as one `text` block.

| Block | Fields | Telegram layout |
|---|---|---|
| `text` | `text` | Paragraph |
| `fields` | `items[{label, value}]` | `<b>Label:</b> value` lines |
| `table` | `columns`, `rows` | Monospaced `<pre>` table, cells padded and truncated to fit |
| `image` | `url` **or** `base64` + `content_type`, optional `caption` | `sendPhoto`; title + text become the caption when short enough, otherwise a follow-up message |
| `code` | `text` | `<pre>` block |
| `link` | `text`, `url` (https only) | Inline keyboard button |
| `question` | `text`, optional `options` (1-10, each up to 64 chars, unique), optional `webhook` (https) | `❓ <b>text</b>` at the end (never truncated away); options become answer buttons, otherwise "Reply to this message to answer." |

Rules: `title` is shown bold at the top; `source` as a small footer; `critical`
adds 🚨 to the title. At most one `question` block. Limits: 20 blocks, 50 table rows × 8 columns, 1 image in the
MVP (inline `base64` up to 5 MB, PNG/JPEG only; use `base64` for images on private
hosts, since Telegram fetches `url` images itself). A channel that can't show a block
falls back to its plain-text form (e.g. a table as aligned text for SMS).
`urgency` defaults to `normal`. Unknown recipients or invalid blocks are a `422`.

## Telegram setup

1. Create a bot with @BotFather; put the token in `RELAY_TELEGRAM_BOT_TOKEN`.
2. Add a recipient: `relay recipients add sara --name "Sara"`.
3. Invite their Telegram account: `relay recipients link sara @sara_tg`. It prints the bot's `t.me` link.
4. They open the bot and tap Start (with `relay serve` running). The bot sees `/start` from `@sara_tg`, links that chat as a verified contact and replies "Linked to Relay as Sara."

Why this is trustworthy: the sender's username in a Telegram update is set by
Telegram's servers, so nobody can send `/start` *as* `@sara_tg`. Only private
chats count. An invite is used once, expires after 7 days (bounding the time
in which a released username could be re-registered by someone else), and one
username can't be invited for two people at once. An existing link keeps
working until the new invite is claimed. Invites are stored encrypted and
expired ones are deleted by the retention job.

Fallback for accounts without a Telegram username: their `/start` gets a link
code (the chat ID encrypted with `RELAY_ENCRYPTION_KEY`, valid for an hour, so
it reveals nothing and can't be forged), and `relay recipients link sara <code>`
links it.

### Answers

`serve` long-polls `getUpdates` (no webhook, so nothing changes in nginx or
Cloudflare). Unconfirmed updates wait on Telegram's side for 24h, so a restart
loses nothing.

- **Button tap:** the button's callback data is `a:<delivery_id>:<option>`. Relay checks the tap came from the chat that delivery went to, saves the option, shows a toast and redraws the buttons with ✅ on the choice (link buttons are kept).
- **Typed reply:** a Telegram reply to the question message, matched by (channel, provider message ID) and the chat. A message that isn't a reply gets a hint; a typed reply to a question with buttons asks for a button.
- **Feedback:** every answer gets a short reply: saved, already final, expired (content purged), or not a question.
- **Webhook:** after a saved answer, Relay POSTs `{"event": "answer", "message_id"}` to the question's `webhook`, retrying after 5s and 30s. Only public addresses are dialed (checked after DNS resolution, so a hostname pointing at a private IP is refused too), redirects aren't followed, and the URL is never logged. Retries live in memory; apps should also poll.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `RELAY_ADDR` | `:8080` | Listen address |
| `RELAY_DB_PATH` | `/data/relay.db` | SQLite file |
| `RELAY_TELEGRAM_BOT_TOKEN` | — | Enables the Telegram channel |
| `RELAY_TELEGRAM_API_URL` | `https://api.telegram.org` | Bot API base URL (tests point it at a fake) |
| `RELAY_WORKER_POLL_INTERVAL` | `1s` | Outbox poll interval |
| `RELAY_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `RELAY_ENCRYPTION_KEY` | — (required) | 32-byte base64 key for private columns. Losing it makes stored content unreadable |
| `RELAY_RETENTION_DAYS` | `30` | Message content is purged after this many days; metadata is kept |
| `RELAY_METRICS_ADDR` | `127.0.0.1:9090` | Internal listener for `/metrics` (and `/debug/pprof` when `RELAY_PPROF=true`) |
| `RELAY_PPROF` | `false` | Serve `/debug/pprof` on the metrics listener |
| `RELAY_MAX_BODY_BYTES` | `7340032` | Request body size limit (7 MB, room for one 5 MB base64 image) |
| `RELAY_TRUST_FORWARDED_FOR` | `false` | Take the client IP from the last `X-Forwarded-For` entry (set by compose, since Relay is only reachable through nginx on the same host) |

## Privacy and security

Relay stores who gets notified about what, which is private. Threat model: a
leaked API key, a leaked DB file or backup, logs shipped somewhere, a malicious
payload (markup injection, oversized or fake images), and a compromised dependency.

- **Encryption at rest:** private columns are encrypted with AES-256-GCM (random nonce per value, key version byte prefix for future rotation). The column and row ID are bound in as associated data, so a ciphertext copied to another row fails to decrypt. Search never needs these columns.
- **Retention:** a daily job in the worker nulls title and blocks and deletes attachments of messages older than `RELAY_RETENTION_DAYS` and marks them `redacted`; message and delivery metadata are deleted after 180 days; expired invites are deleted.
- **Transport:** HTTPS only: Cloudflare terminates public TLS and reaches nginx over TLS (SSL mode Full). Relay listens on the host's loopback only.
- **Auth:** per-client API keys (`rk_` + 32 random bytes), SHA-256 hashed, constant-time compare, revocable. Failed auth is rate-limited per IP: 10 failures in 10 minutes and that IP gets `429` with `Retry-After` until the window ends. Counts live in memory only and IPs are never logged.
- **Input handling:** body size limit, strict JSON decoding, block limits, every value HTML-escaped by the Telegram layout, `link`/`image`/`webhook` URLs restricted to `https`, inline images type-checked by magic bytes. Relay fetches no URLs itself (Telegram fetches image URLs) except question webhooks, which carry no content and may only reach public addresses.
- **Answers:** accepted only from the chat a question was delivered to, encrypted at rest, purged with message content, returned only to the client that asked.
- **Logging:** content and contact addresses are never logged (see Observability).
- **Container:** distroless/static non-root image, read-only root filesystem, only `/data` writable.
- **Supply chain:** `govulncheck` and `gitleaks` in CI, `trivy` scan of the image, Dependabot for Go modules, Actions and the base image; Actions pinned by commit SHA.
- **Backups** (later) are encrypted before leaving the VPS.
- **Review:** the `relay-security-review` project skill is a checklist agents run before PRs touching sensitive areas.

## Observability

Goal: enough to debug any failed delivery from its message ID, without log spam
or leaking content.

- **Logs:** slog JSON to stdout (Docker keeps them, rotate with the json-file driver `max-size`). At `info`: one line per request and one per delivery outcome. Poll ticks and successful inner steps log nothing. Repeated identical errors are rate-limited.
- **Request IDs:** generated (or accepted from `X-Request-ID`), carried in `context`, returned in the response, stored on the message so logs, API and DB line up.
- **Debug path:** `GET /v1/messages/{id}` shows every delivery attempt with its error, so most debugging needs no logs at all.
- **Metrics** (internal listener only): `relay_messages_total{urgency}`, `relay_deliveries_total{channel,result}`, `relay_delivery_duration_seconds{channel}`, `relay_queue_depth`, `relay_oldest_queued_seconds`, plus Go runtime metrics. Labels are low-cardinality only.
- **Health:** `/healthz` (process up, DB reachable); `GET /v1/channels` reports per-channel health.
- **pprof:** off by default, on the internal listener only when enabled.
- Later: Relay alerts ali itself (via Telegram) when the queue backs up or a channel keeps failing.

## Quality tooling

Everything runs through the Makefile, locally and in CI. Tool versions are pinned in the Makefile and installed into `bin/tools` by `make tools`, built with the repo's Go toolchain.

- **golangci-lint** (v2) with, beyond the defaults: `gosec`, `revive`, `gocritic`, `errorlint`, `bodyclose`, `noctx`, `contextcheck`, `sqlclosecheck`, `rowserrcheck`, `nilerr`, `exhaustive`, `unparam`, `misspell`, `funlen`, `gocognit`, `forbidigo` (no `fmt.Print*`/`log.*` outside `cmd/`), `testifylint` if testify is used; formatters `gofumpt` and `goimports`.
- **File length:** `scripts/check-file-length.sh` fails on hand-written `.go` files over 300 lines (tests 500; generated code exempt).
- **Other checks:** `actionlint` for workflows, `hadolint` for the Dockerfile, `sqlc vet` for queries, `go mod tidy` diff check.
- **CI** (`ci.yml`): `make check` plus `make test-e2e` (GitHub runners have Docker), coverage report, image build + `trivy` scan.

## Testing strategy

| Level | Where | What | Runs |
|---|---|---|---|
| Unit | `*_test.go` next to code | Router, renderer, retry, redaction, crypto, config | `make test` |
| Integration | `*_test.go`, real deps in-process | SQLite in `t.TempDir()`, API via `httptest`, Telegram adapter against an `httptest` fake Bot API | `make test` |
| End-to-end | `test/e2e`, `//go:build e2e` | testcontainers: the real image + a fake Telegram API container, driven through the public API (send, retry, idempotency, restart durability) | `make test-e2e`, CI |
| Fuzz | `Fuzz*` tests | JSON request parsing, block validation, layouts | CI short run |

SQLite is embedded, so integration tests use a real file rather than a container;
testcontainers is used where a real process boundary matters (the shipped image,
provider fakes, and later Mailpit for email or Postgres if adopted).

## Deployment

- Target: the VPS reachable as `german-vps`, Docker already installed.
- Files on the VPS in `/opt/relay`: `docker-compose.yml` (copied from `deploy/` on every deploy) and `.env` (created by hand, never in git).
- The VPS already runs nginx on ports 80/443 for other services, so Relay doesn't bring its own proxy. Compose runs one service, `relay` (image `ghcr.io/ali-aliabadi/relay`), with a named volume at `/data`, published only on `127.0.0.1:${RELAY_HOST_PORT:-18080}`. The domain is proxied by Cloudflare (SSL mode Full), so nginx serves `relay.alialiabadi.ir` on 443 with the VPS's existing self-signed origin certificate, restores the client address from `CF-Connecting-IP` (trusted only from Cloudflare's ranges, scoped to this site), and proxies to that port, overwriting `X-Forwarded-For` with that address. The site config is `deploy/nginx-relay.conf`; it's installed by hand once, and deploys never touch nginx.
- **CI** (`.github/workflows/ci.yml`): on pull requests and pushes to `master`, run `make check`, `make test-e2e` and the image scan.
- **CD** (`.github/workflows/deploy.yml`): runs when `ci` passes on a push to `master` (or by hand), and only if that commit is still the head of `master`, so a run finishing late for an older merge never rolls production back. It builds and pushes the image tagged with the commit SHA and `latest`, copies `deploy/` to the VPS over SSH, logs in to GHCR on the VPS with the job's short-lived token (logged out again after the pull), runs `docker compose pull && docker compose up -d` with `RELAY_IMAGE_TAG` set to the SHA, checks the running image is that SHA, then polls `https://relay.alialiabadi.ir/healthz` and fails the job if it is not healthy. Failure output shows container state only, never app logs.
- Compose hardening: the container runs read-only with all capabilities dropped and `no-new-privileges`; logs rotate at 3 × 10 MB.
- GitHub secrets: `VPS_HOST`, `VPS_USER`, `VPS_SSH_KEY` (dedicated deploy key), `VPS_KNOWN_HOSTS` (the VPS host key line, so SSH never trusts on first use), `VPS_PORT` (optional).
- Migrations run on startup, so a deploy is just a restart. Migrations must be backward compatible with the previous release for one deploy.

## Later (not in the MVP)

See the "Later" section of docs/ROADMAP.md. Design notes for the big ones:

- **SMS**: another `Channel` package; makes `high` fallback meaningful. Provider undecided.
- **Quiet hours / digests**: recipient `quiet_hours` + `timezone`; the worker sets `next_attempt_at` to the end of quiet hours for `low`/`normal`.
- **Telegram acks**: a `question` with one "Got it" option covers the ack; unacked `critical` messages escalating is still to do.
- **Self sign-up**: anyone could ask to join via the bot and the admin approves. Today the admin invites by Telegram username instead, which covers two people.
- **MCP server**: exposes `send_message` so Claude and other agents can notify through Relay.
