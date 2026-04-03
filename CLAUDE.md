# claudecodeproxy

Anthropic Messages API proxy that invokes the Claude CLI. Single Go binary.

## Project structure

```
cmd/claudecodeproxy/main.go    # Cobra CLI entrypoint
internal/
  types/                       # Anthropic request types, CLI JSON output types
  claude/                      # CLI runner (with semaphore), model mapping
  converter/                   # Messages→prompt, CLI result→response, SSE streaming
  media/                       # Base64 media→temp files, cleanup
  server/                      # HTTP server, /v1/messages and /health handlers
```

## How it works

1. Receives Anthropic Messages API request at POST /v1/messages
2. Maps model name (claude-sonnet-4 → sonnet)
3. Converts messages to a prompt string (system→`<system>` tags, assistant→`<previous_response>` tags)
4. Media content blocks: base64-decoded to /tmp/claudecodeproxy/, referenced as `[Attached file: /path]`
5. Invokes `claude -p --output-format json --model <m> --dangerously-skip-permissions` with prompt on stdin
6. Parses CLI JSON output, returns Anthropic Messages API response
7. Streaming: uses `--output-format stream-json --verbose --include-partial-messages`, forwards unwrapped SSE events

## Build and test

```bash
go build -o claudecodeproxy ./cmd/claudecodeproxy
go test ./...                                          # mock tests
CLAUDE_E2E=1 go test ./internal/server/ -timeout 180s  # real CLI tests
```

## Release

Tag with `v*` to trigger GoReleaser via GitHub Actions. Publishes:
- GitHub release with tarballs (darwin/linux, amd64/arm64)
- .deb packages
- Homebrew formula to simon0191/homebrew-tap

Requires `HOMEBREW_TAP_TOKEN` secret in GitHub repo settings.
