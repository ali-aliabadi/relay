# Roadmap

Checklist of work for Relay. Agents: take the first unchecked item in the
current phase, and tick it (`[x]`) in the same PR that completes it. Each item
should be one small PR unless noted. Design details are in
[ARCHITECTURE.md](ARCHITECTURE.md).

## Phase 0: one-time setup (ali, by hand)

- [x] Point DNS `relay.alialiabadi.ir` (A/AAAA record) at the VPS
- [ ] Check whether anything on the VPS already uses ports 80/443 (decides Caddy in compose vs. existing proxy)
- [ ] Create the Telegram bot with @BotFather and keep the token
- [ ] On the VPS: create `/opt/relay` and `/opt/relay/.env` with `RELAY_TELEGRAM_BOT_TOKEN` (`chmod 600`)
- [ ] Generate `RELAY_ENCRYPTION_KEY` (`openssl rand -base64 32`), put it in `.env` and keep a copy in a password manager
- [ ] Create a deploy SSH key pair; add the public key to the deploy user's `authorized_keys` on the VPS
- [ ] Add GitHub secrets: `VPS_HOST`, `VPS_USER`, `VPS_SSH_KEY` (and `VPS_PORT` if not 22)
- [ ] Protect `master` (require PRs and green CI)

## Phase 1: skeleton and tooling

- [x] `go mod init github.com/ali-aliabadi/relay`, `cmd/relay/main.go` with a `serve` subcommand
- [x] `internal/config`: parse env vars from the ARCHITECTURE config table, with defaults and validation (fail fast on a missing `RELAY_ENCRYPTION_KEY`)
- [x] `internal/obs`: slog JSON setup, request-ID middleware, request log line, redaction helpers + tests
- [x] `GET /healthz`, graceful shutdown on SIGTERM
- [x] Makefile with `run fmt fmt-check lint test test-e2e sec check generate build docker`
- [x] `.golangci.yml` (v2) with the linter set from ARCHITECTURE "Quality tooling"
- [x] `scripts/check-file-length.sh` (300 / 500 for tests, generated code exempt) wired into `make lint`
- [x] `.gitignore`, `.env.example` (no real values), `.editorconfig`
- [x] Multi-stage Dockerfile: static build, distroless non-root, read-only rootfs friendly; `hadolint` clean
- [x] `.github/workflows/ci.yml`: `make check`, `make test-e2e`, image build + trivy; actions pinned by SHA
- [x] Dependabot for Go modules, GitHub Actions and Docker
- [x] `.claude/settings.json` allowing the `make` targets, so agents run checks without prompts

## Phase 2: storage and privacy foundations

- [x] SQLite open helper (WAL, foreign keys, busy timeout) and goose migrations embedded + run on startup
- [x] Initial migration: `clients`, `recipients`, `contacts`, `messages`, `attachments`, `deliveries`
- [x] sqlc config and queries for each table; `make generate`; `sqlc vet` in lint
- [x] `internal/crypto`: AES-256-GCM field encryption with key-version prefix, tests incl. tamper detection
- [x] Store layer encrypts/decrypts private columns transparently; test that raw DB rows contain no plaintext
- [x] Prefixed ULID helper

## Phase 3: admin CLI and auth

- [ ] `relay clients create|list|revoke` (key printed once, SHA-256 hash stored)
- [ ] Bearer-token auth middleware with constant-time comparison; JSON error helper
- [ ] `relay recipients add|list|remove`

## Phase 4: API

- [ ] Recipients CRUD endpoints
- [ ] `internal/message`: block types (`text`, `fields`, `table`, `image`, `code`, `link`), `text` shorthand, validation and limits
- [ ] `POST /v1/messages`: validate blocks, resolve recipients, store inline image, idempotency, create deliveries, `202`
- [ ] `GET /v1/messages/{id}` and `GET /v1/messages` with filters and cursor pagination
- [ ] `POST /v1/preview` (uses each channel's `Preview`)
- [ ] `GET /v1/channels`

## Phase 5: delivery

- [ ] `Channel` interface, `channel.Error` (permanent / retry-after), `fake` channel
- [ ] Router: urgency + preferences + `channels` override → ordered plan
- [ ] Worker: claim due deliveries atomically, send, record, backoff, requeue stuck `sending` rows on startup
- [ ] Telegram layout: every block type to HTML/`sendPhoto`/inline buttons, escaping, length limits, golden tests
- [ ] Telegram channel: `sendMessage`/`sendPhoto` with HTML parse mode, `disable_notification` for `low`, error classification incl. 429
- [ ] `relay recipients link <username>`: long-poll `getUpdates` for `/start`, store chat_id
- [ ] `relay send` for manual test messages
- [ ] Retention job: purge content after `RELAY_RETENTION_DAYS`, delete metadata after 180 days
- [ ] Metrics on the internal listener (`/metrics`, optional pprof) with the metric set from ARCHITECTURE
- [ ] Integration test: API → worker → fake channel → status `delivered`
- [ ] testcontainers e2e suite: real image + fake Telegram API container (send, retry, idempotency, restart durability)
- [ ] Fuzz tests for request parsing, block validation and layouts

## Phase 6: deploy (MVP done when this is green)

- [ ] `deploy/docker-compose.yml` (relay + caddy, named volume `/data`) and `deploy/Caddyfile`
- [ ] `.github/workflows/deploy.yml`: build + push to GHCR, copy `deploy/` over SSH, `docker compose pull && up -d`, check `/healthz`
- [ ] Docker log rotation (`json-file` `max-size`) in compose
- [ ] Full `relay-security-review` skill pass over the whole codebase before first deploy
- [ ] First production deploy; register ali and ali's wife; send a real message to each
- [ ] README "Running locally" verified against reality

## Later

Roughly in priority order; promote items into a phase when starting them.

- [ ] Nightly encrypted SQLite backup to off-site storage
- [ ] Quiet hours per recipient (timezone-aware), `low` held until morning
- [ ] Daily digest for `low` messages
- [ ] Scheduled / delayed sends (`send_at`)
- [ ] SMS channel (pick a provider first) and real `high` fallback Telegram → SMS
- [ ] Telegram webhook + "Got it" ack button; unacked `critical` escalates
- [ ] Email (SMTP) channel
- [ ] Push channel (ntfy or similar)
- [ ] Self sign-up by username via the bot (`/start <username>`, admin approves)
- [ ] Delivery-status webhooks back to the calling app
- [ ] Per-client rate limits
- [ ] Go client package and a `relay send` CLI usable from other machines
- [ ] MCP server so Claude and other agents can notify through Relay
- [ ] Minimal web dashboard for the message log
- [ ] Relay alerts ali on itself: queue backing up or a channel failing repeatedly
- [ ] Encryption key rotation command (`relay keys rotate`)
- [ ] SessionStart hook so cloud Claude sessions have the right Go, sqlc and golangci-lint
