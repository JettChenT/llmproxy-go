---
name: llmproxy-inspect-cli
description: Use llmproxy-go inspect to query, filter, and debug recent live proxied LLM requests by session ID, including search, model/path/status/code filters, and request detail output.
---

# llmproxy-go inspect CLI skill

Use this skill when you need to inspect live request history captured by a running `llmproxy-go` process.

## When to use

- You have a session ID from the TUI header (for example `sess-abc123...`).
- You need a quick terminal view of recent requests without opening TUI detail panes.
- You want to filter by model/path/status/code or do free-text search in request/response bodies.
- You need one request in full detail for debugging.

## Prerequisites

1. `llmproxy-go` is running.
2. You know the session ID:
   - It is shown in the TUI header.
   - In list view you can press `Shift+Y` to copy it.

## Command patterns

### Show recent matched requests (table)

```bash
./llmproxy-go inspect --session <SESSION_ID> --limit 20
```

### Filter by model/path/status

```bash
./llmproxy-go inspect --session <SESSION_ID> \
  --model gpt-4o \
  --path /chat/completions \
  --status complete
```

### Full-text search in request/response content

```bash
./llmproxy-go inspect --session <SESSION_ID> --search "rate limit"
```

### Filter by HTTP status code

```bash
./llmproxy-go inspect --session <SESSION_ID> --code 429
```

### Inspect one request in detail

```bash
./llmproxy-go inspect --session <SESSION_ID> --request 42
```

### JSON output (for agent parsing/scripts)

```bash
./llmproxy-go inspect --session <SESSION_ID> --json
```

## Flags reference

- `--session` (required): session ID to query.
- `--limit`: number of most recent matched requests to return (`0` = all matched).
- `--request`: show one request by ID in full detail.
- `--search`: case-insensitive full-text search over method/path/url/model/status/body.
- `--model`: case-insensitive model substring match.
- `--path`: case-insensitive endpoint path substring match.
- `--status`: one of `pending`, `complete`, `error`.
- `--code`: exact HTTP status code.
- `--json`: machine-readable output.

## Troubleshooting

- `failed to read session history ... no such file`: session ID is wrong or process has not created history yet.
- Empty results: broaden filters or set `--limit 0` to inspect all matched entries.
- Invalid status: use exactly `pending`, `complete`, or `error`.
