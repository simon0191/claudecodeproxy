# Architecture: claudecodeproxy

## Overview

claudecodeproxy is an HTTP proxy that exposes an [Anthropic Messages API](https://docs.anthropic.com/en/api/messages) endpoint. It supports three operating modes:

| Mode | How it works | Use case |
|------|-------------|----------|
| **cli** | Shells out to `claude -p` | Use Claude Max subscription via CLI |
| **passthrough** | Reverse proxy to `api.anthropic.com` | Auth/billing proxy, no body modification |
| **augmented** | Reverse proxy + injects headers, attribution, metadata | Full Claude Code impersonation |

```mermaid
graph TB
    Client["Any Anthropic SDK Client"] -->|"POST /v1/messages"| Proxy["claudecodeproxy :3456"]

    Proxy -->|"--mode cli"| CLI["claude -p<br/>stdin/stdout"]
    Proxy -->|"--mode passthrough"| API1["api.anthropic.com<br/>(auth headers only)"]
    Proxy -->|"--mode augmented"| API2["api.anthropic.com<br/>(headers + body mods)"]

    CLI -->|"stdout"| Proxy
    API1 -->|"response"| Proxy
    API2 -->|"response"| Proxy
    Proxy -->|"Anthropic API response"| Client
```

## Mode selection

The mode is selected via `--mode` flag or `MODE` env var. Each mode uses a different `messagesHandler` function mounted on the same HTTP server:

```mermaid
flowchart TD
    Start["main.go"] --> Mode{"--mode ?"}

    Mode -->|cli| NewCLI["server.NewCLI()"]
    Mode -->|passthrough| NewPass["server.NewPassthrough()"]
    Mode -->|augmented| NewAug["server.NewAugmented()"]

    NewCLI --> MakeCLI["makeCLIHandler(runner)"]
    NewPass --> PassH["proxy.Passthrough.Handle"]
    NewAug --> AugH["proxy.Augmented.Handle"]

    MakeCLI --> Mux["mux.HandleFunc('POST /v1/messages', handler)"]
    PassH --> Mux
    AugH --> Mux

    Mux --> Server["http.Server.ListenAndServe()"]
```

## Authentication

The proxy supports two auth methods, resolved from environment variables:

```mermaid
flowchart TD
    Resolve["resolveAuth()"] --> OAuth{"CLAUDE_CODE_OAUTH_TOKEN<br/>set?"}
    OAuth -->|Yes| Bearer["AuthConfig{OAuthToken: token}"]
    OAuth -->|No| APIKey{"ANTHROPIC_API_KEY<br/>set?"}
    APIKey -->|Yes| XKey["AuthConfig{APIKey: key}"]
    APIKey -->|No| Err["Error: no credentials"]

    Bearer --> Headers1["Authorization: Bearer token<br/>anthropic-beta: oauth-2025-04-20"]
    XKey --> Headers2["x-api-key: key"]
```

| Auth method | Header | Required beta | Source |
|------------|--------|---------------|--------|
| OAuth (Claude Max) | `Authorization: Bearer <token>` | `oauth-2025-04-20` | `CLAUDE_CODE_OAUTH_TOKEN` |
| API key | `x-api-key: <key>` | none | `ANTHROPIC_API_KEY` |

## Mode: CLI

The original mode. Converts the Anthropic Messages API request into a text prompt and shells out to the `claude` CLI.

```mermaid
sequenceDiagram
    participant C as Client
    participant P as Proxy
    participant CLI as claude -p

    C->>P: POST /v1/messages<br/>{model, messages, system}
    P->>P: Validate & map model<br/>(claude-sonnet-4 -> sonnet)
    P->>P: Flatten messages to prompt string
    P->>P: Decode media to /tmp files

    P->>CLI: stdin: prompt text<br/>flags: --output-format json<br/>--model sonnet
    CLI-->>P: stdout: JSON result
    P->>P: Build Anthropic response
    P-->>C: 200 {id, type, content, usage}
```

**Limitations:**
- Messages are flattened to text (loses structured format)
- Only text responses (no tool_use blocks)
- temperature/max_tokens parsed but not forwarded
- Only 3 models supported (sonnet, opus, haiku)
- Process spawn overhead per request

## Mode: Passthrough

Pure reverse proxy. Forwards the request body unchanged to `api.anthropic.com`, injecting auth and identity headers.

```mermaid
sequenceDiagram
    participant C as Client
    participant P as Proxy
    participant API as api.anthropic.com

    C->>P: POST /v1/messages<br/>{any valid request}
    P->>P: Read request body
    P->>API: POST /v1/messages<br/>+ x-api-key or Authorization<br/>+ anthropic-version<br/>+ User-Agent, x-app, session ID
    API-->>P: Response (JSON or SSE stream)
    P-->>C: Response (forwarded as-is)
```

**Headers injected:**

| Header | Value |
|--------|-------|
| `anthropic-version` | `2023-06-01` |
| `x-api-key` or `Authorization` | From auth config |
| `User-Agent` | `claude-cli/2.1.91 (external, cli)` |
| `x-app` | `cli` |
| `X-Claude-Code-Session-Id` | UUID (stable per proxy instance) |
| `x-client-request-id` | UUID (unique per request) |

Client-provided `anthropic-beta` headers are passed through and merged with any auth-required betas.

## Mode: Augmented

Builds on passthrough by also modifying the request body to match what the real Claude Code CLI sends. This includes the attribution header, system prompt prefix, beta headers, and metadata.

```mermaid
sequenceDiagram
    participant C as Client
    participant P as Proxy
    participant API as api.anthropic.com

    C->>P: POST /v1/messages<br/>{model, messages, system?}

    rect rgb(240, 248, 255)
        Note over P: Request augmentation
        P->>P: Parse request body
        P->>P: Extract model for beta headers
        P->>P: Extract first user message text
        P->>P: Compute fingerprint (SHA256)
        P->>P: Prepend attribution + identity to system prompt
        P->>P: Inject metadata (device_id, session_id)
        P->>P: Set anthropic-beta headers for model
    end

    P->>API: POST /v1/messages<br/>(modified body + all headers)
    API-->>P: Response
    P-->>C: Response (forwarded as-is)
```

### Attribution header

Embedded as the first line of the system prompt (not an HTTP header):

```
x-anthropic-billing-header: cc_version=2.1.91.<fingerprint>; cc_entrypoint=cli;
You are Claude Code, Anthropic's official CLI for Claude.
```

### Fingerprint computation

Replicates the algorithm from `claude-code/utils/fingerprint.ts`:

```mermaid
flowchart LR
    Msg["First user message text"] --> Extract["chars at indices<br/>[4, 7, 20]<br/>('0' if missing)"]
    Extract --> Concat["salt + chars + version"]
    Salt["59cf53e54c78"] --> Concat
    Ver["2.1.91"] --> Concat
    Concat --> Hash["SHA256"]
    Hash --> Slice["First 3 hex chars"]
    Slice --> FP["e.g. 'e96'"]
```

### System prompt injection

The augmented handler prepends the attribution and identity prefix to whatever system prompt the client provided:

```mermaid
flowchart TD
    Input{"Client system<br/>prompt format?"} -->|None| Create["Create array:<br/>[{type: text, text: prefix}]"]
    Input -->|String| Prepend["Prepend prefix + \\n\\n + original"]
    Input -->|"Array of blocks"| Insert["Insert prefix block<br/>at index 0"]
```

### Metadata injection

Added to the request body as:

```json
{
  "metadata": {
    "user_id": "{\"device_id\":\"<uuid>\",\"account_uuid\":\"\",\"session_id\":\"<uuid>\"}"
  }
}
```

### Beta headers (per model)

| Model family | Beta headers injected |
|-------------|----------------------|
| Claude 4+ (non-haiku) | `claude-code-20250219`, `interleaved-thinking-2025-05-14`, `context-management-2025-06-27` |
| Claude 4+ (haiku) | `interleaved-thinking-2025-05-14`, `context-management-2025-06-27` |
| Claude 3.x | `claude-code-20250219` |

## Streaming

All three modes support SSE streaming. The mechanism differs by mode:

```mermaid
flowchart LR
    subgraph CLI Mode
        CLIStream["CLI stdout<br/>(stream_event NDJSON)"] --> Unwrap["Unwrap envelope"] --> SSE1["SSE frames<br/>to client"]
    end

    subgraph Passthrough / Augmented
        APIStream["API response<br/>(SSE stream)"] --> Forward["Copy bytes<br/>+ Flush()"] --> SSE2["SSE frames<br/>to client"]
    end
```

- **CLI mode**: The CLI emits `{"type":"stream_event","event":{...}}` lines. The proxy unwraps the envelope and writes standard SSE frames (`event: ...\ndata: ...\n\n`).
- **Passthrough/Augmented**: The API responds with standard SSE. The proxy copies the response body to the client, flushing after each chunk for low-latency streaming.

## Project structure

```
cmd/claudecodeproxy/
    main.go                    # Cobra CLI: flags, env vars, mode selection
internal/
    proxy/
        proxy.go               # Passthrough handler, AuthConfig, streaming
        augmented.go            # Augmented handler: fingerprint, attribution, metadata, betas
    server/
        server.go              # HTTP server, mode-specific constructors, middleware
        handlers.go            # CLI mode handler (makeCLIHandler), health endpoint
    claude/
        cli.go                 # Runner interface, CLIRunner, subprocess + semaphore
        models.go              # Model name mapping (CLI mode only)
    converter/
        messages.go            # Messages -> CLI prompt string (CLI mode only)
        response.go            # CLI result -> Anthropic response (CLI mode only)
        stream.go              # stream_event unwrapping -> SSE (CLI mode only)
    media/
        media.go               # Base64 decode, temp file save/cleanup (CLI mode only)
    types/
        request.go             # Anthropic request types, Content unmarshaler
        cliresult.go           # CLI JSON output types
```

## Configuration

| Source | Variable | Default | Description |
|--------|----------|---------|-------------|
| Flag | `--mode` | `cli` | Proxy mode: cli, passthrough, augmented |
| Flag | `--port` / `-p` | 3456 | Listen port |
| Flag | `--host` | 127.0.0.1 | Bind address |
| Flag | `--max-concurrent` | 10 | Max concurrent CLI processes (cli mode) |
| Env | `MODE` | `cli` | Proxy mode (overridden by flag) |
| Env | `CLAUDE_CODE_OAUTH_TOKEN` | - | OAuth token (cli + proxy modes) |
| Env | `ANTHROPIC_API_KEY` | - | API key (proxy modes only) |
| Env | `ANTHROPIC_BASE_URL` | `https://api.anthropic.com` | Upstream API URL (proxy modes) |

Precedence: flags > env vars > defaults.

## Testing

```mermaid
flowchart TD
    Tests["go test ./..."] --> ServerTests["internal/server/"]
    Tests --> ProxyTests["internal/proxy/"]
    Tests --> OtherTests["claude/, converter/,<br/>media/, types/"]

    ServerTests --> MockMode{"CLAUDE_E2E=1?"}
    MockMode -->|No| Fake["fakeRunner:<br/>canned responses"]
    MockMode -->|Yes| Real["Real claude CLI"]

    ProxyTests --> MockUpstream["httptest.Server<br/>mock API upstream"]
    MockUpstream --> PassTests["Passthrough tests:<br/>auth, headers, streaming,<br/>errors, forwarding"]
    MockUpstream --> AugTests["Augmented tests:<br/>betas, attribution,<br/>fingerprint, metadata,<br/>system prompt injection"]
```

- `go test ./...` -- all tests, mock mode (~1s)
- `CLAUDE_E2E=1 go test ./internal/server/ -timeout 180s` -- real CLI integration tests
