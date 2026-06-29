# Pi-Bell Project Instructions

## Overview

Pi-Bell is a Raspberry Pi-based doorbell system written in Go. It consists of two main components:

- **bellpush** — Runs on the Pi connected to the physical doorbell button. It detects button presses, captures webcam frames, and exposes a WebSocket-based HTTP server that broadcasts events to connected chimes. It can also fire an HTTP webhook on button press.
- **chime** — Runs on one or more Pis connected to physical door chimes. It connects to the bellpush over WebSocket and activates a relay to ring the chime when a button press event is received. It supports snooze/unsnooze functionality.

Communication between bellpush and chimes happens over the home network using WebSocket connections on the `/doorbell` endpoint.

## Project Structure

```
cmd/
  bellpush/          # Bellpush binary entry point and sub-packages
    bellpush/        # Core bellpush logic (GPIO, camera, event broadcasting, webhook)
    httpserver/      # HTTP server with WebSocket, REST endpoints, templates
    main.go          # Entry point
  chime/
    main.go          # Chime binary (single file, connects to bellpush)
internal/
  pkg/
    events/          # Shared event types (button, snooze, unsnooze, stop-processing)
    pi/              # Raspberry Pi GPIO pin constants
    timeutils/       # Time parsing utilities
scripts/             # Systemd service files, env files, install script
```

## Build & Run

The project uses a `Makefile` for common tasks:

```bash
make run-bellpush              # Run bellpush with GPIO enabled
make run-bellpush-nogpio       # Run bellpush with GPIO disabled (for dev/testing)
make run-bellpush-nogpio-nowebcam  # Run bellpush with GPIO and webcam disabled
make run-chime                 # Run chime (set DOORBELL env var to bellpush address)
make run-chime-nogpio          # Run chime with GPIO disabled
make build-bellpush            # Cross-compile bellpush for ARM (uses zig)
make build-chime               # Cross-compile chime for ARM
make fmt                       # Format Go source files
make checks                    # Run golangci-lint
make build-all                 # Run checks + build both binaries
make release                   # Build all + create tar.gz archive
```

## Testing

Run tests with:

```bash
go test ./...
```

Tests use the standard `testing` package with `httptest` for HTTP testing. No third-party test frameworks are used.

## Linting

The project uses `golangci-lint` (v2 config format) with the following linters enabled:

- errcheck, govet, ineffassign, misspell (US locale), revive, staticcheck, unused
- gofmt formatter enabled

Run linting with:

```bash
golangci-lint run
```

## Coding Conventions

### Go Style

- Follow standard Go conventions (`gofmt` formatting)
- Use US English spelling (enforced by `misspell` linter)
- Module path: `github.com/stuartleeks/pi-bell`
- Go version: 1.19+

### Error Handling

- Wrap errors with `fmt.Errorf("context: %w", err)` for public-facing errors
- Use `log.Printf` for logging within packages
- The codebase uses Application Insights (`appinsights`) for telemetry; track exceptions and events via `telemetryClient`

### Package Layout

- `cmd/` contains executable entry points; each binary is its own directory
- `internal/pkg/` contains shared internal packages (not exported outside the module)
- Sub-packages under `cmd/` contain the core logic (e.g., `cmd/bellpush/bellpush/`, `cmd/bellpush/httpserver/`)

### Configuration

- Configuration is done via environment variables (not config files):
  - `DISABLE_GPIO` — disable GPIO for dev/testing
  - `DISABLE_WEBCAM` — use fake camera frames
  - `WEBCAM_FPS` — webcam frame rate
  - `WEBHOOK_URL` — optional webhook URL for button press notifications
  - `APPINSIGHTS_INSTRUMENTATIONKEY` — Application Insights key
  - `CHIME_NAME` — override chime hostname

### Naming

- Use PascalCase for exported types and functions
- Use camelCase for unexported identifiers
- Struct types use descriptive names (e.g., `BellPush`, `ChimeInfo`, `WebhookNotifier`)
- Constructor functions follow `New<Type>` pattern (e.g., `NewBellPush`, `NewWebhookNotifier`)

### Dependencies

- `gobot.io/x/gobot` — GPIO interaction (uses a fork via `replace` directive)
- `gorilla/websocket` — WebSocket communication
- `go4vl` — Video4Linux2 webcam capture
- `ApplicationInsights-Go` — Telemetry

### Git Commits

- Do not add `Co-authored-by: Copilot` trailers to commit messages

### Target Platform

- Deployment target: Raspberry Pi (ARM, Linux)
- Cross-compilation uses zig as the C compiler for ARM builds
- Development uses a devcontainer (Ubuntu with Go 1.20 and zig)
