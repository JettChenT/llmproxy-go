# AGENTS.md

## Cursor Cloud specific instructions

This is a single-binary Go CLI/TUI application (`llmproxy-go`) — an LLM API debugging proxy with a terminal UI (Bubble Tea). There are no external services, databases, or containers required.

### Build, test, lint

Standard Go commands — see `README.md` for details:
- `go build -o llmproxy-go` — build the binary
- `go test ./...` — run all tests (unit + integration; all self-contained with httptest servers)
- `go vet ./...` — lint

### Running the application

The TUI requires a real terminal (PTY). It will not run in a background shell without a TTY. Use `xfce4-terminal` or the `computerUse` subagent to launch it.

Example with OpenRouter (requires `OPENROUTER_API_KEY` env var):
```
./llmproxy-go -p 8080 -t https://openrouter.ai
```
Then send requests to `http://localhost:8080/api/v1/chat/completions`.

### Gotchas

- The `go.mod` requires **Go 1.25.2**. The environment already has this version installed.
- The proxy target URL should be the API host root (e.g. `https://openrouter.ai`), not a sub-path like `https://openrouter.ai/api`, because the proxy appends the request path to the target.
- The `cost` subcommand and `replay` subcommand are non-TUI and can run without a TTY.
