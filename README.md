# Local Model Router

A transparent, fast, and distributable AI model router that intercepts requests, analyzes them, and routes to the most suitable Ollama model. Drop-in compatible with OpenAI API clients.

## Features

- **OpenAI-Compatible API**: Works with any OpenAI SDK or client
- **Intelligent Routing**: Automatically routes requests to the best model based on content
- **Heuristic Classification**: Fast keyword and pattern-based classification (no extra model calls)
- **API Key Management**: Secure API key storage with SQLite
- **Rate Limiting**: Per-key rate limiting support
- **Admin Dashboard**: Real-time monitoring with htmx/Alpine.js
- **Prometheus Metrics**: Full observability with Prometheus
- **Streaming Support**: Full SSE streaming support
- **Single Binary**: Easy distribution with embedded assets

## Quick Start

### Using Docker Compose (Recommended)

1. Clone the repository:
```bash
git clone https://github.com/kalle/local-model-router.git
cd local-model-router
```

2. Generate secrets:
```bash
# Generate encryption key
openssl rand -hex 32

# Set environment variables
export AUTH_ENCRYPTION_KEY=<your-64-char-hex-key>
export ADMIN_PASSWORD=<your-admin-password>
```

3. Start the services:
```bash
docker-compose up -d
```

4. Pull some models in Ollama:
```bash
docker exec -it ollama ollama pull llama3.2
docker exec -it ollama ollama pull deepseek-coder
```

5. Access the admin UI at http://localhost:8080/admin

### Building from Source

Requirements:
- Go 1.22+
- GCC (for SQLite)

```bash
# Clone
git clone https://github.com/kalle/local-model-router.git
cd local-model-router

# Build
make build

# Run
export AUTH_ENCRYPTION_KEY=$(openssl rand -hex 32)
export ADMIN_PASSWORD=admin
./bin/local-model-router -config config.example.yaml
```

## Usage

### API Endpoints

The router exposes OpenAI-compatible endpoints:

```bash
# Chat completions
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer lmr_your_api_key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "auto",
    "messages": [{"role": "user", "content": "Write a Python function to sort a list"}]
  }'

# With streaming
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer lmr_your_api_key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "auto",
    "stream": true,
    "messages": [{"role": "user", "content": "Explain quantum computing"}]
  }'

# List models
curl http://localhost:8080/v1/models \
  -H "Authorization: Bearer lmr_your_api_key"

# Embeddings
curl http://localhost:8080/v1/embeddings \
  -H "Authorization: Bearer lmr_your_api_key" \
  -H "Content-Type: application/json" \
  -d '{"model": "auto", "input": "Hello world"}'
```

### Using with OpenAI SDK

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="lmr_your_api_key"
)

response = client.chat.completions.create(
    model="auto",  # Let the router decide
    messages=[
        {"role": "user", "content": "Write a quick sort algorithm in Go"}
    ]
)

print(response.choices[0].message.content)
```

### Model Routing

The router classifies requests into categories and selects the best available model:

| Category | Keywords | Preferred Models |
|----------|----------|------------------|
| code | code, function, implement, debug | deepseek-coder, codellama, qwen2.5-coder |
| reasoning | explain, why, analyze, compare | deepseek-r1, llama3.2:70b, mixtral |
| simple | what is, define, list, quick | phi3, llama3.2:1b, tinyllama |
| creative | story, poem, imagine, fiction | llama3.2, mistral |
| math | calculate, solve, equation | deepseek-math, wizard-math |

You can also specify a model directly:
```json
{"model": "llama3.2", "messages": [...]}
```

> **Note:** When `routing.force_routing` is enabled (the default), the `model`
> field in a request is ignored and the router always picks the best model for
> the content. Set `force_routing: false` to honor an explicitly requested model.

### Using with OpenCode

The router is designed to act as a single "virtual model" so you never have to
pick a model again — the router itself is the only model you select, and it
distributes each request to the best available Ollama model.

With `force_routing: true` (default), `GET /v1/models` advertises just one model
(named by `routing.virtual_model_name`, default `auto`). Point OpenCode at the
router as an OpenAI-compatible provider:

```json
{
  "provider": {
    "local-router": {
      "npm": "@ai-sdk/openai-compatible",
      "options": {
        "baseURL": "http://localhost:8080/v1",
        "apiKey": "lmr_your_api_key"
      },
      "models": {
        "auto": { "name": "Local Router (auto)" }
      }
    }
  }
}
```

Select `local-router/auto` in OpenCode. Every request is then routed
intelligently based on its content — no further model selection required.

### Using with VS Code Chat (BYOK)

VS Code's built-in Chat supports **Bring Your Own Key (BYOK)** through a
**Custom Endpoint** provider that speaks the OpenAI Chat Completions API — which
is exactly what the router exposes. This lets you use the router in VS Code Chat
(Ask and Agent modes) **instead of GitHub Copilot models**, without a Copilot
plan or GitHub sign-in.

> **Scope:** BYOK replaces the **chat** experience (Ask, Agent, inline chat, and
> utility tasks). Inline *ghost-text* code completions, semantic search, and
> embeddings remain Copilot-only features.

**1. Create a router API key.** Start the stack (see
[Quick Start](#quick-start) or `./start.ps1`), open the admin UI at
`http://localhost:8080/admin`, sign in as `admin` (with `ADMIN_PASSWORD`), and
create an `lmr_...` key under **API Keys** (copy it — it's shown once).

**2. Register the router as a Custom Endpoint.** In the Chat view, open the model
picker → **Manage Language Models** (gear) → **Add Models** → **Custom
Endpoint**. Choose API type **Chat Completions** and, when
`chatLanguageModels.json` opens, set:

```json
[
  {
    "name": "Local Router",
    "vendor": "customendpoint",
    "apiKey": "${input:lmrApiKey}",
    "apiType": "chat-completions",
    "models": [
      {
        "id": "auto",
        "name": "Local Router (auto)",
        "url": "http://localhost:8080/v1/chat/completions",
        "toolCalling": true,
        "vision": true,
        "streaming": true,
        "maxInputTokens": 32000,
        "maxOutputTokens": 4096
      }
    ]
  }
]
```

Notes:

- `id: "auto"` matches the router's single virtual model. With
  `force_routing: true` the id is ignored anyway and each request is routed by
  content.
- The default auth sends `Authorization: Bearer <apiKey>`, which the router
  expects — no custom header needed. Paste the `lmr_...` key when prompted for
  `${input:lmrApiKey}` (stored in VS Code secret storage).
- `toolCalling: true` makes the model available in **both** Agent and Ask mode.
  Agent mode additionally requires the *backend* model to support tool calling;
  if agent runs misbehave, use Ask mode or set `"toolCalling": false`.
- `vision: true` lets VS Code send images; the router's vision rule forwards
  them to `llava:latest`.

Restart VS Code if **Local Router (auto)** does not appear in the picker.

**3. Route utility tasks to the router** (so titles, commit messages, and intent
detection work without Copilot). In Settings (JSON):

```json
"chat.utilityModel": "Local Router (auto)",
"chat.utilitySmallModel": "Local Router (auto)"
```

**4. Select `Local Router (auto)`** in the chat model picker and start chatting.
Check the admin **Requests** page to see how each message was routed.

> **Remote VS Code:** if VS Code runs in Remote-WSL, SSH, or a Dev Container,
> `localhost:8080` points at the remote host, not the machine running the
> router. Use port forwarding or the router host's IP in `url` instead.

### Documentation lookups via Context7 (OpenCode MCP)

The router focuses on routing requests to the best model. For up-to-date library
and framework documentation, use the Context7 MCP server directly in OpenCode —
it runs independently of the router. Add it to your OpenCode config:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "context7": {
      "type": "remote",
      "url": "https://mcp.context7.com/mcp"
    }
  }
}
```

To get higher rate limits with a free Context7 account, pass your API key (with
`CONTEXT7_API_KEY` set in your environment):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "context7": {
      "type": "remote",
      "url": "https://mcp.context7.com/mcp",
      "headers": { "CONTEXT7_API_KEY": "{env:CONTEXT7_API_KEY}" }
    }
  }
}
```

Then add `use context7` to a prompt when you want current docs. See the
[OpenCode MCP docs](https://opencode.ai/docs/mcp-servers/#context7) for details.

## Configuration

See `config.example.yaml` for all options. Key settings:

```yaml
server:
  host: "0.0.0.0"
  port: 8080

ollama:
  endpoint: "http://localhost:11434"
  max_connections: 100          # HTTP connection-pool size (warm keep-alive reuse)

auth:
  enabled: true
  # Set AUTH_ENCRYPTION_KEY env var

admin:
  enabled: true
  username: "admin"
  # Set ADMIN_PASSWORD env var
  path: "/admin"

routing:
  default_model: "auto"
  force_routing: true            # Always route by content, ignore requested model
  virtual_model_name: "auto"     # Single model name advertised by /v1/models
  capabilities:
    code:
      keywords: ["code", "function", "implement"]
      prefer_models: ["deepseek-coder", "codellama"]
```

## Admin Dashboard

Access the admin UI at `http://localhost:8080/admin`:

- **Dashboard**: System status, request stats, model usage
- **Models**: View available Ollama models
- **API Keys**: Create and manage API keys
- **Requests**: View recent request history

## Metrics

Prometheus metrics are available at `/metrics`:

- `lmr_requests_total` - Total requests by endpoint/status
- `lmr_request_duration_seconds` - Request latency histogram
- `lmr_tokens_processed_total` - Tokens by model and type
- `lmr_model_requests_total` - Requests per model
- `lmr_classification_results_total` - Classification by category
- `lmr_routing_decisions_total` - Routing decisions by reason
- `lmr_force_routing_enabled` - Whether force routing is enabled (1/0)
- `lmr_model_overrides_total` - Requests where the requested model was overridden by force routing

## Architecture

```
┌─────────────┐     ┌──────────────────────────────────────┐     ┌─────────┐
│   Client    │────▶│         Local Model Router           │────▶│  Ollama │
│ (OpenAI SDK)│     │                                      │     │         │
└─────────────┘     │  ┌──────────┐  ┌──────────────────┐  │     │ ┌─────┐ │
                    │  │ Auth &   │  │   Classifier     │  │     │ │model│ │
                    │  │ Rate     │  │   (Heuristic)    │  │     │ │model│ │
                    │  │ Limiter  │  │                  │  │     │ │model│ │
                    │  └──────────┘  └──────────────────┘  │     │ └─────┘ │
                    │         │              │             │     └─────────┘
                    │         ▼              ▼             │
                    │  ┌──────────┐  ┌──────────────────┐  │
                    │  │ Key Store│  │   Model Router   │  │
                    │  │ (SQLite) │  │                  │  │
                    │  └──────────┘  └──────────────────┘  │
                    └──────────────────────────────────────┘
```

## License

MIT License - see LICENSE file for details.
