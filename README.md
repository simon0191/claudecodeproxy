# claudecodeproxy

An HTTP proxy that exposes an [Anthropic Messages API](https://docs.anthropic.com/en/api/messages) endpoint and fulfills requests by invoking the [Claude CLI](https://docs.anthropic.com/en/docs/claude-code) under the hood. Use your Claude Max subscription as an API.

```
Client (any Anthropic API client)
    │
    │  POST /v1/messages
    ▼
claudecodeproxy (:3456)
    │
    │  stdin/stdout
    ▼
claude CLI (--print --output-format json)
```

## Features

- Full [Anthropic Messages API](https://docs.anthropic.com/en/api/messages) compatibility (streaming and non-streaming)
- Multi-modal support: images, audio, video, and documents via base64
- Configurable concurrency limit for CLI processes
- Single static binary, no runtime dependencies beyond the Claude CLI

## Install

### Homebrew (macOS)

```bash
brew tap simon0191/homebrew-tap
brew install claudecodeproxy
```

### Debian/Ubuntu

Download the `.deb` from the [latest release](https://github.com/simon0191/claudecodeproxy/releases/latest):

```bash
curl -LO https://github.com/simon0191/claudecodeproxy/releases/latest/download/claudecodeproxy_<version>_linux_amd64.deb
sudo dpkg -i claudecodeproxy_<version>_linux_amd64.deb
```

### From source

```bash
go install github.com/simon0191/claudecodeproxy/cmd/claudecodeproxy@latest
```

## Prerequisites

- [Claude CLI](https://docs.anthropic.com/en/docs/claude-code) installed and authenticated
- `CLAUDE_CODE_OAUTH_TOKEN` environment variable set (run `claude setup-token` to generate one)

## Usage

```bash
# Start with defaults (localhost:3456)
claudecodeproxy

# Custom port and host
claudecodeproxy --port 8080 --host 0.0.0.0

# Limit concurrent CLI processes
claudecodeproxy --max-concurrent 5
```

### Configuration

| Flag | Env var | Default | Description |
|---|---|---|---|
| `--port`, `-p` | `PORT` | 3456 | Listen port |
| `--host` | `HOST` | 127.0.0.1 | Bind address |
| `--max-concurrent` | `MAX_CONCURRENT` | 10 | Max concurrent CLI processes |
| | `CLAUDE_CODE_OAUTH_TOKEN` | (required) | Claude CLI authentication token |

Flags take precedence over environment variables.

### API

| Endpoint | Method | Description |
|---|---|---|
| `/v1/messages` | POST | Anthropic Messages API |
| `/health` | GET | Health check |

### Example request

```bash
curl -X POST http://localhost:3456/v1/messages \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

### Supported models

| API model | CLI model |
|---|---|
| `claude-sonnet-4` | `sonnet` |
| `claude-opus-4` | `opus` |
| `claude-haiku-4` | `haiku` |

### Media support

Send images, audio, video, or documents as base64-encoded content blocks:

```bash
curl -X POST http://localhost:3456/v1/messages \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4",
    "max_tokens": 1024,
    "messages": [{
      "role": "user",
      "content": [
        {"type": "text", "text": "What is in this image?"},
        {"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "iVBOR..."}}
      ]
    }]
  }'
```

The proxy saves media to a temp directory and passes file paths to the CLI.

## Testing

```bash
# Unit tests (mock CLI, fast)
go test ./...

# End-to-end tests (real Claude CLI, requires auth)
CLAUDE_E2E=1 go test ./internal/server/ -timeout 180s -v
```

## Architecture

See [architecture.md](architecture.md) for detailed design documentation with Mermaid diagrams.

## License

MIT
