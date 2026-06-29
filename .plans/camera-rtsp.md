# Camera RTSP Feed

## Problem

The camera is currently only accessible via a polling HTTP endpoint (`GET /camera/latest`) which returns a single JPEG frame. This works for the web UI but doesn't support standard video clients (VLC, Home Assistant, NVR software, etc.). We want to expose the camera as an RTSP stream while preserving all existing behaviour.

## Constraints

- `GET /camera/latest` must continue returning the latest JPEG frame
- The web page auto-refresh (polling `/camera/latest` every 1s) must continue working
- Fake camera mode (`DISABLE_WEBCAM=true`) should also work over RTSP
- The RTSP feed should be optional/disableable via configuration

## Approach

Use [`gortsplib`](https://github.com/bluenviron/gortsplib) to run an in-process RTSP server. The existing V4L2 capture loop already produces JPEG frames; we publish each frame to the RTSP server as an RTP/MJPEG stream. The `/camera/latest` endpoint remains unchanged — it still reads the latest frame from the shared buffer.

### Architecture

```
/dev/video0 (V4L2 MJPEG)
       │
       ▼
  capture loop (existing)
       │
       ├──► webcamFrame []byte ──► GET /camera/latest (existing, unchanged)
       │
       └──► RTSP publisher ──► gortsplib server ──► RTSP clients (new)
                                   (port 8554)
```

### Why gortsplib

- Pure Go, no CGo — cross-compiles cleanly with the existing zig toolchain
- Supports MJPEG over RTP (RFC 2435) — matches our existing frame format
- Actively maintained, used by mediamtx internally
- In-process: no IPC, no extra daemons to manage

## Key Design Decisions

### RTSP port

Default to `8554` (standard RTSP port). Configurable via `RTSP_PORT` env var. Set to empty or `0` to disable RTSP entirely.

### Stream path

`rtsp://<host>:8554/camera` — single stream, matching the single camera.

### Authentication

No auth initially (matches existing HTTP endpoints which have no auth). Can be added later via gortsplib's auth hooks.

### Frame publishing

Each frame from the capture loop is published to the RTSP server. If no RTSP clients are connected, frames are simply discarded by the library (no resource waste). The existing `webcamFrame` buffer write happens as before — the RTSP publish is an additional step in the same goroutine.

### Fake camera mode

The fake camera loop also publishes to RTSP, so testing works end-to-end without a real device.

## Implementation Todos

### 1. Add gortsplib dependency

Run `go get github.com/bluenviron/gortsplib/v4` and update go.mod/go.sum.

### 2. Create RTSP server package (`cmd/bellpush/rtspserver/`)

New package with:
- `RTSPServer` struct wrapping `gortsplib.Server`
- `NewRTSPServer(port int) *RTSPServer` — creates and configures the server
- `Start() error` — starts listening in a goroutine
- `PublishFrame(jpegData []byte)` — wraps JPEG in RTP/MJPEG and pushes to connected clients
- `Stop()` — graceful shutdown
- Handle client describe/setup/play callbacks

### 3. Integrate into BellPush struct (`cmd/bellpush/bellpush/bellpush.go`)

- Add an optional `rtspPublish func([]byte)` callback field (or an interface)
- In the camera capture goroutine, after writing `webcamFrame`, call the publish callback if set
- Same for fake camera loop

### 4. Wire up in main (`cmd/bellpush/main.go`)

- Read `RTSP_PORT` env var (default `"8554"`, empty/`"0"` to disable)
- If enabled, create `RTSPServer`, start it, and pass its `PublishFrame` to `BellPush`
- On shutdown, stop the RTSP server

### 5. Update Makefile / build

- Verify cross-compilation still works (gortsplib is pure Go, should be fine)
- No new CGo dependencies expected

### 6. Update README

- Document `RTSP_PORT` env var
- Document how to connect (e.g. `vlc rtsp://<pi-ip>:8554/camera`)

## Files Changed

| File | Change |
|------|--------|
| `go.mod` / `go.sum` | Add `gortsplib/v4` dependency |
| `cmd/bellpush/rtspserver/rtspserver.go` | **New** — RTSP server wrapper |
| `cmd/bellpush/bellpush/bellpush.go` | Add frame publish callback, call in capture loops |
| `cmd/bellpush/main.go` | Read config, create & wire RTSP server |
| `README.md` | Document RTSP_PORT and usage |

## Notes

- No changes to `httpServer.go` or `templates/index.html` — existing behaviour is fully preserved
- gortsplib handles RTP packetization of JPEG frames (RFC 2435) internally
- If FPS is low (default 2), the RTSP stream will appear choppy — users may want to increase `WEBCAM_FPS` when using RTSP
- Future enhancement: add H.264 encoding for better compression/compatibility (would require CGo or a hardware encoder)
- Future enhancement: add RTSP authentication
- Testable locally with `DISABLE_GPIO=true DISABLE_WEBCAM=true RTSP_PORT=8554`
