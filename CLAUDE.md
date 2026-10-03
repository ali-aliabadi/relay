# CLAUDE.md

Guidance for Claude Code (and other agents) working in this repo, locally or in the cloud.

## What this is

Relay is ali's personal notification gateway: a small Go HTTP service where apps
`POST /v1/messages` with recipients, urgency and content blocks (text, fields, table,
image, code, link, question), and Relay formats them with the channel's one built-in layout, routes it to a channel (Telegram in the MVP), retries and logs it.
Answers to a `question` come back through the bot and apps fetch them from `GET /v1/messages/{id}/answers`.
Users are ali and ali's wife, registered by hand.

**Relay handles private data.** Message content, images and contact details
(Telegram chat IDs, later phone numbers) are personal. Privacy rules below are not optional.

- Design reference: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). Read it before non-trivial work.
- What to build next: [docs/ROADMAP.md](docs/ROADMAP.md). Pick the first unchecked item in the current phase unless told otherwise.

## Stack (decided, don't re-litigate without asking)

Go (stdlib `net/http`, no web framework) · SQLite via `modernc.org/sqlite` (pure Go, keep `CGO_ENABLED=0`)
· `sqlc` for queries · `goose` migrations embedded and run on startup · `log/slog` · Prometheus metrics
· env-var config · Docker + docker compose on a VPS behind its existing nginx · GitHub Actions deploy on push to `master`.

SMS is deliberately **not** in the MVP; the provider is undecided.

## Layout

```
cmd/relay/            main: subcommands serve, migrate, clients, recipients, send, version
internal/api/         handlers, auth middleware, request/response types
internal/core/        router, renderer, worker, retry policy, retention
internal/message/     content block types, validation, limits
internal/channel/     channel.go (interface + Error), answer.go, telegram/ (client, layout, poller), fake/
internal/store/       migrations/*.sql, queries/*.sql, generated sqlc code
internal/crypto/      field encryption (AES-256-GCM) for private columns
internal/id/          prefixed ULIDs
internal/obs/         logging setup, redaction, metrics, request IDs
internal/config/      env parsing
test/e2e/             testcontainers tests against the built Docker image
scripts/              check-file-length.sh and other repo checks
deploy/               docker-compose.yml (copied to the VPS by CD), nginx-relay.conf (installed by hand)
.github/workflows/    ci.yml, deploy.yml
.claude/skills/       project skills (relay-security-review)
```

## Commands

The Makefile is the single entry point; CI runs the same targets.

```bash
make tools        # install pinned dev tools into bin/tools (run once)
make run          # go run ./cmd/relay serve with ./data/relay.db
make fmt          # gofumpt + goimports
make lint         # golangci-lint + file-length check + actionlint + hadolint
make test         # unit + integration tests, -race, with coverage
make test-e2e     # testcontainers tests (needs Docker)
make sec          # govulncheck + gitleaks (+ trivy on the image)
make check        # fmt-check, lint, test, sec: run this before every commit
make generate     # sqlc generate (commit the generated code)
make build        # static binary in ./bin/relay
make docker       # build the image locally
```

If a `make` target doesn't exist yet, the roadmap item that creates it hasn't
landed; use the plain `go` equivalent.

## Code size limits

Small files keep agent context focused and diffs reviewable.

- Hand-written `.go` files: **max 300 lines**. `_test.go` files: max 500. Generated code is exempt.
- Enforced by `scripts/check-file-length.sh` in `make lint` and CI.
- Functions: golangci-lint `funlen` (~60 lines) and `gocognit` limits.
- When a file hits the limit, split by responsibility (e.g. `messages_create.go`, `messages_list.go`), not by arbitrary halves. Don't raise limits or add exemptions without asking ali.

## Conventions

- **Layering:** `api` → `core` → `store`/`channel`. Handlers never call channels; only `store` writes SQL.
- **Errors:** wrap with `fmt.Errorf("doing x: %w", err)`. API errors always use `{"error": {"code", "message"}}`, and never echo private input back.
- **IDs:** prefixed ULIDs (`msg_`, `rcp_`, `cli_`, `dlv_`).
- **Time:** store UTC, RFC 3339 in JSON. Inject a clock (`func() time.Time`) where time matters.
- **Context:** every function doing I/O takes `ctx context.Context` first.
- **Dependencies:** prefer the stdlib. Adding a module needs a one-line reason in the PR description.
- **Migrations:** never edit a merged migration; add a new one. Keep them compatible with the previous release.
- **No templates.** There are no per-app or per-message templates. Each channel has one layout in code that renders the generic blocks; don't add a template store or per-app formatting. New needs become a new block type (ask ali first).
- **Layouts escape everything:** every caller value is escaped (`html.EscapeString` for Telegram); callers never send markup.
- **No `fmt.Print*` or `log.*`** outside `cmd/`; use the injected `*slog.Logger` (enforced by `forbidigo`).

## Testing

Three levels, all required for new behaviour:

- **Unit** (`foo_test.go`, no build tag): table-driven, pure logic (router, renderer, retry, redaction, config). Use the `fake` channel and an injected clock.
- **Integration** (no build tag, still `make test`): real SQLite in `t.TempDir()`, API through `httptest` against the real router and store, Telegram channel against an `httptest` server that mimics the Bot API.
- **End-to-end** (`//go:build e2e`, `make test-e2e`): testcontainers starts the real Docker image plus a fake Telegram API container, then drives the public API. Later channels add containers (e.g. Mailpit for email).
- Also: fuzz tests for request parsing and layouts; golden files (`testdata/*.golden`, `-update` flag) for each layout's output per block type.
- Never call real Telegram or any real provider in tests. Never put real chat IDs, phone numbers or tokens in fixtures.
- Every bug fix comes with a test that fails before the fix.
- Cloud sessions may have no Docker: run `make test` and say e2e was not run, rather than skipping it silently.

## Privacy and security (always on)

- **Never log** message title/blocks, images, rendered text, contact addresses, API keys or tokens, at any level. Log IDs instead. Use `obs.Redact` helpers for anything user-supplied.
- **Encrypt at rest:** message title/blocks, image bytes and contact addresses go through `internal/crypto` before hitting SQLite. Key from `RELAY_ENCRYPTION_KEY`.
- **Retention:** message content is purged after `RELAY_RETENTION_DAYS`; don't add new places that keep content longer.
- **Secrets:** never commit them. Runtime secrets live in `/opt/relay/.env` on the VPS; CI secrets in GitHub. gitleaks runs in CI.
- **API keys:** shown once, stored as SHA-256 hashes, compared in constant time.
- **Inputs:** request body size limits, strict JSON decoding (`DisallowUnknownFields`), validate everything at the API boundary.
- **Before opening a PR** that touches `api/`, `store/`, `crypto/`, `channel/`, logging, config, Docker or workflows: run the `relay-security-review` project skill (`.claude/skills/relay-security-review/SKILL.md`) and fix what it finds.

## Observability (useful, not noisy)

- `slog` JSON to stdout. Default level `info`.
- At `info`: one line per HTTP request (request_id, client, method, route pattern, status, duration) and one per delivery outcome (message_id, delivery_id, channel, result, attempt). Nothing per poll tick, nothing on success paths inside loops.
- `warn` for retried failures, `error` only for things needing a human. Repeated identical errors are rate-limited.
- `debug` may add detail but still follows the never-log rules above.
- Metrics at `/metrics` (internal only, not exposed through nginx) with **low-cardinality labels only** (channel, urgency, status; never recipient, client key or message ID).
- Request IDs flow through `context` and appear in every related log line and in the `X-Request-ID` response header.

## Working agreement

- Branch from `master`, open a PR; merging to `master` **deploys to production**.
- Keep docs in sync in the same PR: tick boxes in docs/ROADMAP.md, update docs/ARCHITECTURE.md if the design changed, README.md if usage changed.
- Small PRs, one roadmap item (or a few tightly related ones) each.
- If a design question isn't answered in docs/ARCHITECTURE.md, ask ali rather than guessing on anything user-visible.

## Local vs cloud sessions

- **Local** (ali's machine): may have SSH access as `german-vps`. Still, never deploy or change the VPS by hand unless ali asks; deployment goes through GitHub Actions.
- **Cloud** sessions: no access to the VPS or its secrets. Go may be older than `go.mod` requires; if `go` complains, say so instead of downgrading `go.mod`. Docker may be unavailable (see Testing).
