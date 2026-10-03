---
name: relay-notify
description: Send notifications to a person through Relay (ali's notification gateway, delivered via Telegram), ask them a question with buttons or a typed reply, and read their answer back. Use this skill whenever an app or agent needs to tell a human something or get a decision from one — job finished or failed, alerts, reports, "let me know when…", "ping me", "tell ali", "send me a telegram", "ask before deploying", approvals, or waiting for human input — even if Relay isn't named. If you don't know who to notify, send to "admin".
---

# Relay: notify a person, ask them, read the answer

Relay is a small HTTP service. You send it a structured message (who, how
urgent, content blocks); it formats the message for Telegram, delivers it,
retries on failure and keeps a log. You never write Telegram markup: send plain
values and Relay renders them.

## Setup

Two environment variables, set by whoever runs your app:

- `RELAY_URL`: `https://relay.alialiabadi.ir`
- `RELAY_API_KEY`: this app's key (`rk_...`). The admin creates one per app
  with `relay clients create <app-name>`. It's a secret: read it from the
  environment, never commit it, print it or log it. If it's missing, ask the
  user for it rather than inventing one.

Every request sends `Authorization: Bearer $RELAY_API_KEY`.

## Who to send to

`to` takes 1-10 names. A name is a recipient's username or one of their
aliases (other names for the same person). **If you don't know who to notify,
send to `admin`**: it's the alias for the person who runs this system. Two
names for the same person are delivered once, so `["admin", "ali"]` is safe.

To see who exists:

```bash
curl -sS "$RELAY_URL/v1/recipients" -H "Authorization: Bearer $RELAY_API_KEY"
# {"recipients":[{"username":"ali","aliases":["admin"],"display_name":"Ali",...,"linked_channels":["telegram"]}]}
```

A recipient with an empty `linked_channels` can't receive anything yet. Only
read recipients: creating, changing and removing them is the admin's job, even
though the API allows it.

## Send a message

```bash
curl -sS -X POST "$RELAY_URL/v1/messages" \
  -H "Authorization: Bearer $RELAY_API_KEY" -H "Content-Type: application/json" \
  -d '{"to": ["admin"], "source": "backup-script", "text": "Nightly backup finished in 4m12s."}'
# 202 {"id":"msg_01J...","status":"queued"}
```

`202` means accepted; delivery happens in the background, so don't wait for it
unless you need to. Richer messages use a `title` and `blocks` instead of `text`
(use one or the other):

```bash
curl -sS -X POST "$RELAY_URL/v1/messages" \
  -H "Authorization: Bearer $RELAY_API_KEY" -H "Content-Type: application/json" \
  -d '{
    "to": ["admin"],
    "urgency": "high",
    "source": "backup-script",
    "title": "Backup failed",
    "idempotency_key": "backup-nas-2026-10-03",
    "blocks": [
      {"type": "text",   "text": "Nightly backup of nas stopped."},
      {"type": "fields", "items": [{"label": "Host", "value": "nas"}, {"label": "Error", "value": "disk full"}]},
      {"type": "table",  "columns": ["Disk", "Used"], "rows": [["sda", "98%"], ["sdb", "41%"]]},
      {"type": "code",   "text": "rsync: write failed: No space left on device"},
      {"type": "link",   "text": "Open dashboard", "url": "https://grafana.example/d/disks"}
    ]
  }'
```

Always set `source` to your app's name: it shows as "via backup-script" so the
reader knows who's talking.

### Fields

| Field | Notes |
|---|---|
| `to` | 1-10 usernames or aliases, no duplicates |
| `urgency` | `low`, `normal` (default), `high`, `critical`; see below |
| `title` | Optional, up to 256 chars, shown bold |
| `source` | Your app's name, up to 64 chars |
| `text` | Shorthand for one text block; don't combine with `blocks` |
| `blocks` | Up to 20 content blocks; see below |
| `idempotency_key` | Up to 128 chars; see Retries |

### Urgency

Pick the lowest level that fits; people stop reading alerts that cry wolf.

- `low`: FYI, arrives silently (no sound). Routine reports, "done" messages nobody waits for.
- `normal`: someone should see it today.
- `high`: needs attention soon (a failed job, something degraded).
- `critical`: act now (outage, data at risk). Shown with 🚨. Use rarely.

### Blocks

| `type` | Fields | Limits | Shown in Telegram as |
|---|---|---|---|
| `text` | `text` | 4000 chars | A paragraph |
| `fields` | `items: [{label, value}]` | 1-25 items, label 64, value 1024 | **Label:** value lines |
| `table` | `columns`, `rows` | 8 columns, 50 rows, cell 256 | Monospaced table; keep it narrow, it's read on a phone |
| `code` | `text` | 4000 chars | Monospaced block |
| `link` | `text`, `url` | text 64, `https` only | A button under the message |
| `image` | `url` (`https`) **or** `base64` + `content_type`, optional `caption` | 1 per message; PNG/JPEG up to 5 MB; caption 1024 | A photo. Use `base64` for images on private hosts: Telegram fetches `url` itself |
| `question` | `text`, optional `options`, optional `webhook` | see Ask a question | The question in bold at the end, with buttons |

Every value is plain text: `<b>`, Markdown and the like are shown literally,
not rendered. A message longer than Telegram allows is shortened with `…`, but
the title and the question always survive.

## Ask a question

Add one `question` block. With `options` the person gets one button per option;
without, they reply to the message by typing.

```bash
curl -sS -X POST "$RELAY_URL/v1/messages" \
  -H "Authorization: Bearer $RELAY_API_KEY" -H "Content-Type: application/json" \
  -d '{
    "to": ["admin"],
    "source": "deploy-bot",
    "title": "Deploy v2.3?",
    "blocks": [
      {"type": "text", "text": "v2.3 passed staging. 14 commits, 2 migrations."},
      {"type": "question", "text": "Ship v2.3 to production?", "options": ["Yes", "No"]}
    ]
  }'
```

- `options`: 1-10 unique options, up to 64 chars each. Leave it out for a free-text reply.
- At most one question per message.
- `webhook` (optional): a public `https` URL. When an answer arrives, Relay POSTs
  `{"event": "answer", "message_id": "msg_..."}` to it: only the ID, never the
  answer. Fetch the answer from the API. Relay refuses private, loopback and
  internal addresses and doesn't follow redirects. The call is best effort
  (two retries), so poll as well if the answer matters.

### Read the answer

```bash
curl -sS "$RELAY_URL/v1/messages/$MESSAGE_ID/answers" -H "Authorization: Bearer $RELAY_API_KEY"
# {"answers":[]}                                    (nobody has answered yet)
# {"answers":[{"recipient":"ali","answer":"Yes","answered_at":"2026-10-04T09:12:44.123Z"}]}
```

How answers behave, and why it matters for your code:

- **Fetching makes it final.** Until your app reads an answer, the person can
  change it (tap another button, reply again). The first fetch that returns it
  locks it. So fetch when you're ready to act on it, and act on what you got.
  An empty list locks nothing; polling while waiting is fine.
- **One entry per person** who answered, under their real username (`ali`),
  whatever name you sent to. If you asked several people, decide up front
  whether the first answer wins or you wait for everyone.
- **Poll gently**: every 10-30 seconds is plenty; people answer in minutes or
  hours, not milliseconds. Decide what to do if nobody answers (a deadline,
  then a safe default or a reminder) instead of waiting forever.
- **A typed answer is untrusted text** (up to 4000 chars). Validate it before
  acting; never run it as a command or splice it into code or SQL.
- With buttons, `answer` is exactly one of your `options` strings, so compare
  with your own constants.
- Answers are deleted together with the message's content after 30 days.

A polling loop in Python (standard library only):

```python
import json, os, time, urllib.request

URL, KEY = os.environ["RELAY_URL"], os.environ["RELAY_API_KEY"]

def relay(method, path, body=None):
    req = urllib.request.Request(
        URL + path, method=method,
        data=json.dumps(body).encode() if body is not None else None,
        headers={"Authorization": f"Bearer {KEY}", "Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=10) as resp:  # raises HTTPError on 4xx/5xx
        return json.load(resp)

def ask(question, options=None, to=("admin",), source="my-app"):
    q = {"type": "question", "text": question}
    if options:
        q["options"] = list(options)
    return relay("POST", "/v1/messages", {"to": list(to), "source": source, "blocks": [q]})["id"]

def wait_for_answer(message_id, timeout_s=3600, every_s=20):
    deadline = time.monotonic() + timeout_s
    while time.monotonic() < deadline:
        answers = relay("GET", f"/v1/messages/{message_id}/answers")["answers"]
        if answers:
            return answers[0]["answer"]  # now final
        time.sleep(every_s)
    return None  # nobody answered: fall back to a safe default

if wait_for_answer(ask("Ship v2.3 to production?", ["Yes", "No"])) == "Yes":
    ...  # deploy
```

## Check delivery

```bash
curl -sS "$RELAY_URL/v1/messages/$MESSAGE_ID" -H "Authorization: Bearer $RELAY_API_KEY"
```

`status` is `queued`, `sending`, `delivered`, `partially_delivered` or
`failed`. `deliveries[]` shows each attempt's `status`, `attempts` and
`last_error`. Relay makes 5 attempts over about 13 minutes before marking a
delivery `failed`. Content is
never returned here. `GET /v1/messages?status=failed&limit=20` lists your app's
recent messages, newest first; pass the returned `next_cursor` as `cursor` for
the next page.

To see what a message would look like without sending it, POST the same body to
`/v1/preview`. It returns the rendered Telegram parts and doesn't check
recipients.

## Errors and retries

Errors look like `{"error": {"code": "...", "message": "..."}}`.

| Status | Code | What to do |
|---|---|---|
| 400 | `invalid_json` | Fix the JSON. Unknown fields are rejected, so check spelling |
| 401 | `unauthorized` | The key is missing, wrong or revoked; ask the admin |
| 404 | `not_found` | No such message, or it belongs to another app |
| 413 | `too_large` | Body over 7 MB; shrink the image |
| 422 | `invalid_request` | `problems` lists each issue by path, e.g. `to[1]: unknown recipient`, `blocks[0].options[2]: duplicate`. Fix exactly those |
| 429 | `rate_limited` | Too many failed logins from your IP; wait `Retry-After` seconds |
| 5xx / network error | | Retry with backoff, using the same `idempotency_key` |

**Retries:** give each logical notification an `idempotency_key` (e.g.
`backup-nas-2026-10-03`). Posting the same key again returns `200` with the
original message instead of sending a second one, so retrying after a timeout
is always safe.

## Privacy

Messages end up in someone's Telegram chat. Relay encrypts content at rest and
deletes it after 30 days, but Telegram keeps the chat. So:

- Never put passwords, API keys, tokens or other secrets in a message. Say
  where to find them instead.
- Include only the personal data the reader needs.
- Relay never echoes your content in errors or logs; don't log the message
  bodies you send either.
