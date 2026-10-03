---
name: relay-security-review
description: Security and privacy review for Relay changes. Use before opening a PR that touches api/, store/, crypto/, channel/, logging, config, Dockerfile, compose or workflows, or when asked for a security check.
---

# Relay security and privacy review

Relay stores private data: who gets notified, about what, and how to reach them.
Review the diff (`git diff master...HEAD`) against this checklist, then the files it
touches in full. Report findings as a list with `file:line`, severity
(critical / high / medium / low) and the fix. Fix critical and high before the PR.

Also run: `make sec` (govulncheck, gitleaks) and `make lint` (includes gosec).

## 1. Private data never leaks

- [ ] No log line (any level) contains message title/blocks, image bytes, rendered text, contact addresses (chat IDs, phones, emails), API keys, bot tokens or the encryption key. Look at new `slog` calls, `%v`/`%+v` of structs, and errors that wrap provider responses (Telegram errors can echo the text).
- [ ] Errors returned to API clients don't echo private input or internal details (SQL, stack traces, file paths).
- [ ] Metrics labels contain no recipient, client, message ID or content.
- [ ] Test fixtures and golden files contain no real chat IDs, phone numbers or tokens.
- [ ] New struct fields holding private data have a redacting `LogValue()` / `String()` or are never logged.

## 2. Encryption and retention

- [ ] New columns holding content (blocks, image bytes) or contact details are encrypted via `internal/crypto`.
- [ ] No plaintext copy of private data is written elsewhere (cache, temp file, extra column, log, metric).
- [ ] The retention job still covers every place content is stored.
- [ ] Migrations don't decrypt data into plaintext columns.

## 3. Auth and access

- [ ] Every new `/v1` route sits behind the auth middleware. Only `/healthz` is public.
- [ ] Key comparison stays constant-time; keys are never stored or logged in plaintext.
- [ ] A client can only read messages it created (no ID enumeration across clients).
- [ ] `/metrics` and pprof stay on the internal listener, not routed through Caddy.

## 4. Input handling

- [ ] Request bodies go through the size limit and strict JSON decoding.
- [ ] Every field is validated (block types and limits, enums, recipient existence) before storage.
- [ ] SQL only through sqlc queries; no string-built SQL.
- [ ] Layouts escape every caller value (`html.EscapeString` for Telegram); no caller-supplied markup reaches a provider.
- [ ] `link` and `image` URLs are `https` only; Relay never fetches caller URLs itself; inline images are size-capped and type-checked by magic bytes.
- [ ] Outbound HTTP uses a client with timeouts and a context; provider URLs come from code/config, never from request input (SSRF).

## 5. Secrets and config

- [ ] No secrets in code, tests, `.env.example`, compose files or workflow YAML.
- [ ] New secrets are read from env and documented in ARCHITECTURE's config table.
- [ ] Startup fails fast when a required secret is missing or malformed.

## 6. Container, CI and supply chain

- [ ] Image stays non-root, static, minimal; no new writable paths except `/data`.
- [ ] Workflows: actions pinned by SHA, least-privilege `permissions:`, no secrets in logs, no `pull_request_target` with checkout of PR code.
- [ ] New dependencies are justified, maintained, and pass `govulncheck`.

## 7. Abuse and availability

- [ ] New endpoints can't trigger unbounded work (pagination limits, fan-out limits on recipients).
- [ ] Retries are bounded and honour provider `retry_after`.
