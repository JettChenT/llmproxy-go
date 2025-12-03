# llmproxy-go

A lightweight debugging proxy for LLM API requests with a beautiful terminal UI. Inspect, record, and replay your LLM API calls with ease.

## Features

- 🔍 **Request Inspection** - View all LLM API requests/responses in real-time with a rich TUI
- 💰 **Cost Tracking** - Automatic token usage and cost calculation for popular models
- 💾 **Smart Caching** - Memory and persistent caching with configurable TTL
- 📼 **Session Recording** - Record sessions to "tape files" and replay them later
- 🔎 **Search & Filter** - Quickly find requests with fuzzy search
- 📊 **Sortable Views** - Sort by duration, tokens, cost, status, and more
- 🎯 **Provider Detection** - Automatic detection of OpenAI, Anthropic, and other providers
- ⚡ **Zero Configuration** - Works out of the box with OpenAI-compatible APIs

## Installation

### Download Pre-built Binaries

Download the latest release for your platform from the [releases page](https://github.com/JettChenT/llmproxy-go/releases):

**macOS (Apple Silicon):**
```bash
curl -L https://github.com/JettChenT/llmproxy-go/releases/latest/download/llmproxy-go_Darwin_arm64.tar.gz | tar xz
sudo mv llmproxy-go /usr/local/bin/
```

**macOS (Intel):**
```bash
curl -L https://github.com/JettChenT/llmproxy-go/releases/latest/download/llmproxy-go_Darwin_x86_64.tar.gz | tar xz
sudo mv llmproxy-go /usr/local/bin/
```

**Linux (x86_64):**
```bash
curl -L https://github.com/JettChenT/llmproxy-go/releases/latest/download/llmproxy-go_Linux_x86_64.tar.gz | tar xz
sudo mv llmproxy-go /usr/local/bin/
```

**Linux (ARM64):**
```bash
curl -L https://github.com/JettChenT/llmproxy-go/releases/latest/download/llmproxy-go_Linux_arm64.tar.gz | tar xz
sudo mv llmproxy-go /usr/local/bin/
```

**Windows:**
Download the appropriate `.zip` file from the [releases page](https://github.com/JettChenT/llmproxy-go/releases) and extract it.

### Build from Source

```bash
git clone https://github.com/JettChenT/llmproxy-go.git
cd llmproxy-go
go build -o llmproxy-go
```

## Quick Start

1. **Start the proxy:**
```bash
llmproxy-go --listen :8080 --target https://api.openai.com
```

2. **Point your application to the proxy:**
```bash
export OPENAI_BASE_URL=http://localhost:8080/v1
# or for some libraries:
export OPENAI_API_BASE=http://localhost:8080
```

3. **Make LLM API calls from your application** - they'll now appear in the llmproxy-go TUI!

## Usage

### Basic Command

```bash
llmproxy-go [flags]              # Start the proxy server
llmproxy-go --config config.toml # Start with config file (supports multiple proxies)
llmproxy-go --gen-config         # Print example configuration to stdout
llmproxy-go cost <tape-file>     # Print cost breakdown for a tape file
```

### Command-Line Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | - | Path to TOML config file for multi-proxy configuration |
| `--gen-config` | - | Generate an example configuration file and exit |
| `--listen` | `:8080` | Address to listen on (e.g., `:8080`, `0.0.0.0:8080`) |
| `--target` | `http://localhost:3000` | Target URL to proxy to (e.g., `https://api.openai.com`) |
| `--tape` | - | Open a tape file for inspection (replay mode) |
| `--save-tape` | - | Auto-save session to tape file |
| `--cache` | `none` | Cache mode: `none`, `memory`, or `global` |
| `--cache-ttl` | `24h` | Cache TTL duration (e.g., `1h`, `24h`, `7d`) |
| `--cache-simulate-latency` | `false` | Simulate original response latency for cached responses |
| `--cache-dir` | `~/.llmproxy-cache` | Directory for persistent cache storage |

### Examples

**Proxy OpenAI API calls:**
```bash
llmproxy-go --listen :8080 --target https://api.openai.com
```

**Enable caching to speed up development:**
```bash
llmproxy-go --listen :8080 --target https://api.openai.com \
  --cache global --cache-ttl 24h
```

**Record a session for later analysis:**
```bash
llmproxy-go --listen :8080 --target https://api.openai.com \
  --save-tape debug-session.tape
```

**Replay a recorded session:**
```bash
llmproxy-go --tape debug-session.tape
```

**Analyze costs from a recorded session:**
```bash
llmproxy-go cost debug-session.tape
```

**Proxy Anthropic API:**
```bash
llmproxy-go --listen :8080 --target https://api.anthropic.com
```

**Cache with simulated latency (for realistic testing):**
```bash
llmproxy-go --listen :8080 --target https://api.openai.com \
  --cache global --cache-simulate-latency
```

## Multi-Proxy Mode

Run multiple proxies simultaneously, each forwarding to a different target. This is useful when working with multiple LLM providers at once.

### Using Config File

Generate an example config file:
```bash
llmproxy-go --gen-config > config.toml
```

Start with config file:
```bash
llmproxy-go --config config.toml
```

### Config File Format (TOML)

```toml
# Multiple proxies - each [[proxy]] section defines one proxy instance
[[proxy]]
name = "openai"
listen = ":8080"
target = "https://api.openai.com"

[[proxy]]
name = "anthropic"
listen = ":8081"
target = "https://api.anthropic.com"

[[proxy]]
name = "local"
listen = ":8082"
target = "http://localhost:11434"

# Cache configuration (shared across all proxies)
[cache]
mode = "memory"          # "none", "memory", or "global"
ttl = "24h"              # Supports "1h", "24h", "7d", etc.
simulate_latency = false
# dir = "/path/to/cache" # Only for "global" mode

# Optional: auto-save all sessions to a tape file
# save_tape = "session.tape"
```

### Config Options

#### Proxy Settings

| Field | Required | Description |
|-------|----------|-------------|
| `name` | No | Human-readable name for the proxy (shown in TUI) |
| `listen` | Yes | Address to listen on (e.g., `:8080`) |
| `target` | Yes | Target URL to proxy to |

#### Cache Settings

| Field | Default | Description |
|-------|---------|-------------|
| `mode` | `none` | Cache mode: `none`, `memory`, or `global` |
| `ttl` | `24h` | Cache TTL (e.g., `1h`, `24h`, `7d`) |
| `simulate_latency` | `false` | Simulate original response latency |
| `dir` | `~/.llmproxy-cache` | Directory for persistent cache |

### Multi-Proxy TUI

In multi-proxy mode, the TUI displays:
- A **PROXY** column showing which proxy handled each request
- Status bar showing all active proxies: `openai(:8080→openai.com) anthropic(:8081→api.anthropic.com)`

Point your applications to the appropriate proxy port:
```bash
# OpenAI SDK → port 8080
export OPENAI_BASE_URL=http://localhost:8080/v1

# Anthropic SDK → port 8081  
export ANTHROPIC_BASE_URL=http://localhost:8081
```

## Keyboard Shortcuts

### List View

| Key | Action |
|-----|--------|
| `j` / `k` or `↓` / `↑` | Navigate up/down |
| `g` / `G` | Jump to first/last request |
| `[number]j/k` | Move N rows (e.g., `10j` moves down 10 rows) |
| `:N` | Jump to request ID N (e.g., `:42` jumps to request #42) |
| `Enter` | View request details |
| `/` | Search/filter requests |
| `f` | Toggle follow mode (auto-scroll to latest) |
| `s` | Save current session to tape file |
| `q` | Quit |

**Sortable columns** (click header or use mouse):
- ID, Status, Model, Status Code, Size, Duration, Input Tokens, Output Tokens, Cost

### Detail View

| Key | Action |
|-----|--------|
| `1-4` | Switch between tabs (Messages, Output, Raw Input, Raw Output) |
| `Tab` / `h` | Next tab |
| `Shift+Tab` / `l` | Previous tab |
| `j` / `k` or `↓` / `↑` | Scroll content |
| `g` / `G` | Jump to top/bottom |
| `n` / `N` | Navigate to next/previous message |
| `c` | Collapse/expand current message |
| `C` | Collapse/expand all messages |
| `Esc` or `q` | Close detail view |

### Tape Playback Mode

| Key | Action |
|-----|--------|
| `Space` | Play/pause |
| `[` / `]` | Step backward/forward |
| `0` / `$` | Jump to start/end |
| `+` / `-` | Increase/decrease playback speed (1x, 2x, 4x, 8x, 16x) |
| `r` | Toggle real-time vs step-through mode |
| `f` | Toggle follow mode |

## Caching Modes

### `none` (default)
No caching. Every request goes through to the target.

### `memory`
Cache responses in memory for the duration of the proxy session. Fast but lost when proxy restarts.

### `global`
Persistent cache stored on disk using BadgerDB. Cache persists across proxy restarts.

**Benefits:**
- Speed up development by avoiding repeated identical API calls
- Save API costs during testing
- Work offline with previously cached responses

**Cache Key Generation:**
The cache key is generated from the request path and body, ensuring identical requests return the same cached response.

## Use Cases

### 1. Debugging LLM Applications
See exactly what prompts your application is sending and what responses it's getting in real-time.

### 2. Cost Monitoring
Track token usage and costs across different models and providers.

### 3. Testing and Development
Use caching to avoid hitting rate limits and reduce costs during development. Record sessions to create reproducible test cases.

### 4. Performance Analysis
Measure response times, identify bottlenecks, and optimize your LLM integrations.

### 5. Prompt Engineering
Quickly iterate on prompts by seeing the full conversation history in a readable format.

### 6. Team Collaboration
Share tape recordings with teammates to reproduce issues or demonstrate behavior.

## Configuration with Different LLM Clients

### OpenAI Python SDK
```python
import openai

client = openai.OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="your-api-key"
)
```

### OpenAI Node.js SDK
```javascript
import OpenAI from 'openai';

const openai = new OpenAI({
  baseURL: 'http://localhost:8080/v1',
  apiKey: process.env.OPENAI_API_KEY,
});
```

### LangChain (Python)
```python
from langchain.chat_models import ChatOpenAI

llm = ChatOpenAI(
    openai_api_base="http://localhost:8080/v1",
    openai_api_key="your-api-key"
)
```

### cURL
```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "model": "gpt-4",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

### Anthropic SDK (Python)
```python
import anthropic

client = anthropic.Anthropic(
    base_url="http://localhost:8080",
    api_key="your-api-key"
)
```

## Advanced Features

### Search and Filtering

Press `/` to enter search mode. The search filters requests by:
- Request ID
- Model name
- Request/response body content
- Status codes

Press `Esc` to clear the filter or `Enter` to apply it.

### Session Recording

Save sessions to tape files for later analysis:

```bash
# Record a session
llmproxy-go --listen :8080 --target https://api.openai.com --save-tape session.tape

# Or save interactively by pressing 's' in the TUI
```

Replay recorded sessions:
```bash
llmproxy-go --tape session.tape
```

Tape files are useful for:
- Creating reproducible test cases
- Debugging issues
- Sharing examples with teammates
- Analyzing performance over time

### Cost Tracking

llmproxy-go automatically calculates costs for popular models from:
- OpenAI (GPT-4, GPT-3.5, etc.)
- Anthropic (Claude models)
- Google (Gemini models)
- And more...

Costs are displayed in the main view and can be sorted/filtered.

### Cost Breakdown Command

Analyze the total cost of a recorded session with the `cost` command:

```bash
llmproxy-go cost session.tape
```

This outputs a table showing:
- Cost breakdown by model
- Request count per model
- Input/output token totals
- Total session cost

## Tips and Tricks

1. **Use follow mode** (`f`) when monitoring live traffic to always see the latest requests
2. **Record sessions** during important debugging sessions with `--save-tape`
3. **Enable caching** during development to speed up iteration and reduce costs
4. **Use search** (`/`) to quickly find specific requests by model or content
5. **Jump to request IDs** using `:N` syntax (e.g., `:42`) for quick navigation
6. **Sort by cost** to identify expensive requests
7. **Collapse messages** (`c`/`C`) in detail view for better overview of long conversations
