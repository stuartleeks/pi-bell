# Bellpush Webhook Support

## Problem

When the doorbell is pressed, external systems (e.g. home-dash) have no way to be notified other than via the existing WebSocket chime protocol. We need to add an HTTP webhook that fires on button press so that push notifications can be sent to phones/browsers.

## Approach

Add a fire-and-forget webhook POST to a configurable URL on each button press event, integrated into the existing `BellPush.BroadcastEvent` flow. The webhook is a new concern within the bellpush package — no new dependencies required (stdlib `net/http`).

## Key Design Decisions

### Snooze behaviour

The webhook fires **regardless of chime snooze state**. The webhook targets mobile push notifications — users can mute notifications on their device if desired. This keeps the bellpush logic simpler (no snooze check needed for the webhook path).

### Where the webhook fires

The webhook is triggered inside `BroadcastEvent` (not at each call site). This ensures all button sources (GPIO, stdin, HTTP endpoint) are covered consistently.

### Separation of concerns

The webhook client lives in its own file (`webhook.go`) within the bellpush package. This keeps the core `bellpush.go` clean and makes the webhook logic independently testable.

## Implementation Todos

### 1. Add webhook client (`cmd/bellpush/bellpush/webhook.go`)
Create a new file with a `WebhookNotifier` struct that:
- Holds the target URL and an `*http.Client` with a 5-second timeout
- Has a `Notify()` method that POSTs the hardcoded JSON payload
- Logs at info level when firing, at error/warning level on failure
- Tracks failures via Application Insights telemetry client (if provided)
- Is a no-op if URL is empty (constructor returns nil, callers nil-check)

### 2. Wire webhook into BellPush struct (`cmd/bellpush/bellpush/bellpush.go`)
- Add a `webhook *WebhookNotifier` field to `BellPush`
- Update `NewBellPush` to accept the webhook notifier (or a webhook URL string)
- In `BroadcastEvent`: if event is `ButtonPressed`, fire `webhook.Notify()` in a goroutine

### 3. Read config and pass to BellPush (`cmd/bellpush/main.go`)
- Read `WEBHOOK_URL` env var
- Log at info level whether webhook is enabled/disabled (and the URL if enabled)
- Create `WebhookNotifier` and pass to `NewBellPush`

### 4. Add unit tests (`cmd/bellpush/bellpush/webhook_test.go`)
- Test that `Notify()` sends correct method, content-type, and body to an `httptest.Server`
- Test that a nil `WebhookNotifier` or empty URL is a safe no-op

### 5. Update README
- Document the `WEBHOOK_URL` environment variable

## Files Changed

| File | Change |
|------|--------|
| `cmd/bellpush/bellpush/webhook.go` | **New** — WebhookNotifier struct and Notify method |
| `cmd/bellpush/bellpush/webhook_test.go` | **New** — Unit tests for webhook |
| `cmd/bellpush/bellpush/bellpush.go` | Add webhook field, wire into BroadcastEvent |
| `cmd/bellpush/main.go` | Read WEBHOOK_URL, create notifier, pass to NewBellPush |
| `README.md` | Document WEBHOOK_URL env var |

## Notes

- No new Go dependencies — uses stdlib `net/http`
- Backward-compatible: empty/missing `WEBHOOK_URL` means webhook is disabled
- Payload is hardcoded for v1; templating can be added later if needed
- Testable locally with `DISABLE_GPIO=true DISABLE_WEBCAM=true WEBHOOK_URL=http://localhost:9999/test`
