# Camera ONVIF / WebRTC / HLS via go2rtc

## Goal

Expose the doorbell camera to **UniFi Protect (UDM SE)** for adoption/recording, and
provide a **low-latency live view (WebRTC, with HLS fallback)** in the bellpush web UI,
while keeping the existing snapshot contract (`GET /camera/latest`) working for the
external doorbell-notifier project.

Minimise CPU/memory: no software video transcoding in the steady state.

## Background research

### The camera

`v4l2-ctl --list-formats-ext` on the Pi shows the CSI camera (IMX219-class) advertises a
hardware **`H264`** capture format (alongside `MJPG`, `JPEG`, raw YUV). H.264 is produced
by the **VideoCore GPU encoder**, so emitting H.264 costs ~0% ARM CPU. This is the ideal
case: H.264 can be **packetised/passed through** without any software encode.

### UniFi Protect ingest options compared

| Option | Gets into Protect | Maintained | Process model | Transcode | Browser view | Notes |
|---|---|---|---|---|---|---|
| **unifi-cam-proxy** | ✅ UVC-camera emulation | ❌ stale | Python container on separate host | usually yes (ffmpeg) | no | Deep UniFi integration but unmaintained, transcodes |
| **Native ONVIF in bellpush** | ✅ third-party ONVIF | self-owned | in-process (pure Go) | no | no (would need MediaMTX too) | Lowest footprint *only* if browser view not needed; large SOAP/WS-Discovery/WS-Security code surface to own |
| **go2rtc sidecar** ⟵ chosen | ✅ third-party ONVIF (go2rtc ONVIF **server**) | ✅ very active | separate pure-Go binary | no | ✅ built-in WebRTC/MSE/HLS | One binary covers ONVIF + RTSP + WebRTC + HLS + snapshot |
| MediaMTX | ❌ not by itself | active | sidecar | no | yes | Restreamer only; no ONVIF server for adoption |
| Scrypted | ❌ wrong direction | active | sidecar | — | yes | Pulls *from* Protect; does not ingest into it |

**Decision:** **go2rtc as a sidecar on the Pi.** Rationale:

- Includes an **ONVIF server** (discoverable/adoptable by ONVIF clients) — the role Protect's
  third-party-camera support needs. (Protect not in go2rtc's explicitly-tested client list →
  must be validated against the UDM SE; see Risks.)
- Provides **WebRTC + MSE + HLS** out of the box for the web UI (the deciding factor — we want
  browser live view, and the in-process-ONVIF alternative would require adding MediaMTX anyway,
  i.e. two Go runtimes vs go2rtc's one).
- Provides an HTTP **snapshot** endpoint (`/api/frame.jpeg`) to back `/camera/latest`.
- Can own the camera directly via its **`exec`** source (`rpicam-vid`/`ffmpeg`), getting the
  hardware H.264 stream and fanning it out to every consumer with no transcode.
- Pure Go, single multi-arch binary (arm/v6/v7/arm64), actively maintained — directly answers
  the "unifi-cam-proxy is stale" concern.

### CPU/memory trade-off (why a sidecar is acceptable)

- Steady-state video cost is essentially identical to an in-process design: GPU does the encode,
  no transcode, Protect holds a persistent RTSP pull either way.
- go2rtc adds a second Go runtime (~20–40 MB RSS) plus the capture child (~10–20 MB).
- **At the feature parity we actually want (incl. browser WebRTC/HLS)**, the in-process
  alternative would be *bellpush + MediaMTX* = two runtimes anyway, so go2rtc (one runtime doing
  ONVIF+RTSP+WebRTC+HLS+snapshot) is competitive or lighter. WebRTC SRTP costs CPU only while a
  browser is actively viewing (typically one viewer).

## Architecture

go2rtc owns the camera device and is the single source of truth for all video. bellpush keeps
its doorbell logic and delegates all camera concerns to go2rtc.

```
rpicam-vid (HW H.264)
     │  (go2rtc `exec` source)
     ▼
  go2rtc ───┬── ONVIF server + RTSP  ─────────────► UniFi Protect (adopt + record)
            ├── WebRTC / MSE / HLS   ─────────────► bellpush web UI (<video-stream>)
            └── GET /api/frame.jpeg  ─┐
                                      │
bellpush  ── GET /camera/latest ──────┘ (reverse-proxy/fetch snapshot)
          ── doorbell button / chimes / webhook (unchanged)
```

**Key consequence — device ownership:** only one process may hold the V4L2 H.264 encoder, so
when go2rtc owns the camera, **bellpush must stop capturing**. bellpush's in-process capture
(`StartCameraCapture`) and the existing `rtspserver` package become redundant for the go2rtc
path. They are retained only as a fallback/dev path (see config toggle).

## Configuration

New bellpush env vars (read in `cmd/bellpush/main.go`):

| Var | Default | Meaning |
|---|---|---|
| `GO2RTC_URL` | _empty_ | Base URL of the go2rtc sidecar API, e.g. `http://localhost:1984`. When set, bellpush delegates camera to go2rtc (no in-process capture, no built-in RTSP). When empty, legacy in-process behaviour is preserved. |
| `GO2RTC_STREAM` | `doorbell` | go2rtc stream name to use for snapshot/live view. |

When `GO2RTC_URL` is set:
- bellpush does **not** call `StartCameraCapture` / `StartFakeCameraCapture` and does **not**
  start `rtspserver` (avoids device contention with go2rtc).
- `/camera/latest` proxies `${GO2RTC_URL}/api/frame.jpeg?src=${GO2RTC_STREAM}`.
- The web UI renders the go2rtc WebRTC/HLS player.

This toggle keeps existing deployments and the `DISABLE_WEBCAM` dev/test path working unchanged
when `GO2RTC_URL` is unset.

### go2rtc config (`go2rtc.yaml`, deployed alongside bellpush)

```yaml
streams:
  doorbell:
    # Hardware H.264 straight from the Pi camera, no transcode.
    - exec:rpicam-vid -t 0 --inline --width 1280 --height 720 --framerate 15 --codec h264 --nopreview -o - #killsignal=9
    # Dev/test fallback without a camera (uncomment to use a test pattern):
    # - exec:ffmpeg -hide_banner -re -f lavfi -i testsrc -c:v libx264 -f mpegts -#video=h264

api:
  listen: ":1984"      # HTTP API + WebUI: WebRTC/HLS/MSE, snapshot, AND ONVIF server
rtsp:
  listen: ":8554"      # RTSP server (ONVIF advertises this for the video stream)
webrtc:
  listen: ":8555"      # WebRTC TCP/UDP
# No separate ONVIF listener: go2rtc serves ONVIF on the api port (:1984) under
# /onvif/device_service, and does not answer WS-Discovery (add manually by URL).
```

Notes:
- Keep `framerate` ≥ 15 so Protect's timeline/motion behaves well.
- `/api/frame.jpeg` produces JPEG from the H.264 stream via go2rtc's bundled ffmpeg; this is
  infrequent (doorbell press + occasional web refresh) so the transcode cost is negligible.
- ONVIF/RTSP is TCP-only in go2rtc — fine for Protect.

## Implementation phases

### Phase 1 — go2rtc deployment & validation (no app code)

1. Add `go2rtc.yaml` (above) under `scripts/`.
2. Add a systemd unit `scripts/pibell-go2rtc.service` (mirrors `pibell-bellpush.service`):
   `ExecStart` the go2rtc binary with `-config /usr/local/bin/pi-bell/go2rtc.yaml`,
   `Restart=always`, `After=network.target`.
3. Update `scripts/install.sh` to download the go2rtc release binary for the Pi arch into the
   install folder (or document manual install) and enable the new service.
4. **Validate before touching app code:**
   - Live view + snapshot: `http://<pi>:1984/` WebUI, `…/api/frame.jpeg?src=doorbell`.
   - ONVIF discovery with ONVIF Device Manager / go2rtc's own WebUI "Add" (cheap debug loop).
   - **Adopt in UniFi Protect** (third-party camera) → confirms the linchpin risk.

#### Verification steps (UDM Protect adoption)

A cheap-to-expensive ladder — each step builds on the last so you fail fast before
touching Protect. Leave `GO2RTC_URL` unset and stop the bellpush service first so it
isn't holding the camera device.

0. **Free the camera** (only one process may own `/dev/video0`):

   ```bash
   sudo systemctl stop pibell-bellpush
   ```

1. **Camera command works standalone (Buster / `raspivid`):**

   ```bash
   raspivid -t 2000 -w 1280 -h 720 -fps 15 -n -ih -o /tmp/test.h264 && ls -l /tmp/test.h264
   ```

   If empty/errors, fix the capture command in `go2rtc.yaml` before anything else.
   (Bullseye → `libcamera-vid`, Bookworm → `rpicam-vid`.)

2. **go2rtc starts and ingests** — run it in the foreground and watch the log for the
   `doorbell` stream producing H.264 (no ffmpeg transcode warnings):

   ```bash
   /usr/local/bin/pi-bell/go2rtc -config /usr/local/bin/pi-bell/go2rtc.yaml
   ```

3. **Snapshot + browser live view** (proves the web-UI half of the feature):
   - Open `http://<pi>:1984/` → click the `doorbell` stream → confirm live video.
   - `curl -o /tmp/f.jpg "http://<pi>:1984/api/frame.jpeg?src=doorbell"` → valid JPEG.

4. **ONVIF server check (cheap debug loop, before Protect)** — isolates "ONVIF broken"
   from "Protect-specific" issues. go2rtc serves ONVIF on the **API port `:1984`** at
   `/onvif/device_service` (there is **no** separate ONVIF port), and it does **not**
   answer WS-Discovery, so clients must be pointed at the URL manually:
   - Direct SOAP probe (no auth needed) — expect a SOAP XML response, not 405:

     ```bash
     curl -s -X POST http://<pi>:1984/onvif/device_service \
       -H 'Content-Type: application/soap+xml; charset=utf-8' \
       -d '<?xml version="1.0" encoding="UTF-8"?>
     <s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
       <s:Body xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
         <tds:GetSystemDateAndTime/>
       </s:Body>
     </s:Envelope>'
     ```

   - **ONVIF Device Manager** (free Windows tool): add the device manually with URL
     `http://<pi>:1984/onvif/device_service` (auto-discovery will not find it).

5. **Adopt in UniFi Protect (the linchpin):**
   - go2rtc has no WS-Discovery responder, so Protect won't auto-find it — add it
     **manually** as a third-party / ONVIF camera.
   - **CONFIRMED on UDM SE:** Protect's "add camera" dialog labels the field as an
     *IP address*, but it accepts `host:port` — enter **`<pi>:1984`** (e.g. `pibell-0:1984`)
     so Protect reaches go2rtc's ONVIF service. Entering only the IP makes Protect assume
     port 80 and fail with a misleading "invalid credentials" error.
   - **Auth:** go2rtc 1.9.4 validates HTTP Basic only and does **not** parse the ONVIF
     WS-Security `UsernameToken` that Protect sends, so setting `api.username`/`password`
     causes "invalid credentials". Leave go2rtc auth **unset** and enter any **dummy**
     username/password in Protect (they are ignored by an unauthenticated go2rtc).
   - Keep the UDM SE and Pi reachable to each other (same subnet avoids routing/firewall
     surprises even though discovery isn't used).
   - Confirm: live view appears, then a recording clip is captured.

6. **Only after adoption succeeds**, wire bellpush in (`GO2RTC_URL=http://localhost:1984`)
   and re-enable the service. Verify `/camera/latest` still returns a JPEG and the web UI
   shows the WebRTC player.

### Phase 2 — bellpush snapshot delegation (`/camera/latest`)

Files: `cmd/bellpush/main.go`, `cmd/bellpush/httpserver/httpServer.go`.

- Read `GO2RTC_URL` / `GO2RTC_STREAM` in `main.go`; pass into the HTTP server (constructor or a
  setter, following existing patterns).
- In `httpCameraLatest`: if `GO2RTC_URL` set, fetch `${GO2RTC_URL}/api/frame.jpeg?src=${stream}`
  (short timeout) and stream the JPEG back, preserving the existing `image/jpeg` + `no-store`
  contract so the external notifier project is unaffected. On error, return 502.
- When `GO2RTC_URL` set, skip `StartCameraCapture`/`StartFakeCameraCapture` and skip starting
  `rtspserver` in `main.go` (guard the existing blocks).

### Phase 3 — web UI live view (WebRTC + HLS fallback)

File: `cmd/bellpush/httpserver/templates/index.html`.

- Replace the polling `<img src="/camera/latest">` block (and its `updateWebcam` /
  auto-refresh JS) with go2rtc's live player when go2rtc is enabled. Pass a template field
  (e.g. `Go2rtcBaseURL`, `Go2rtcStream`) from the handler.
- Embed go2rtc's `<video-stream>` web component (`video-rtc.js`), which negotiates **WebRTC**
  first and falls back to **MSE/HLS** automatically:
  ```html
  <script src="{{ .Go2rtcBaseURL }}/video-rtc.js" type="module"></script>
  <video-stream src="{{ .Go2rtcBaseURL }}/api/ws?src={{ .Go2rtcStream }}"></video-stream>
  ```
- Keep a "snapshot" button using `/camera/latest` for a still image.
- **Origin/port:** simplest is to expose go2rtc `:1984` to the browser. Optional hardening:
  reverse-proxy `/go2rtc/` from bellpush to go2rtc so the UI is single-origin (note in README).
- When `GO2RTC_URL` is unset, fall back to the existing `<img>` polling block (template `if`).

### Phase 4 — docs & cleanup

- README: document go2rtc sidecar, `GO2RTC_URL`/`GO2RTC_STREAM`, Protect adoption steps, and
  browser view. Update the RTSP section to note go2rtc is now the streaming front end.
- Consider **deprecating/removing the in-process `rtspserver`** once go2rtc is the default
  (nothing currently consumes the built-in RTSP feed). Keep for one release behind the toggle,
  then remove `cmd/bellpush/rtspserver/` and the `SetOnFrame`/capture wiring.

## Files changed

| File | Change |
|---|---|
| `scripts/go2rtc.yaml` | **New** — go2rtc stream/ONVIF/RTSP/WebRTC config |
| `scripts/pibell-go2rtc.service` | **New** — systemd unit for the sidecar |
| `scripts/install.sh` | Download go2rtc binary + enable service |
| `cmd/bellpush/main.go` | Read `GO2RTC_URL`/`GO2RTC_STREAM`; skip capture+rtspserver when set |
| `cmd/bellpush/httpserver/httpServer.go` | `/camera/latest` proxies go2rtc snapshot; pass template fields |
| `cmd/bellpush/httpserver/templates/index.html` | WebRTC/HLS `<video-stream>` player (with legacy fallback) |
| `README.md` | Document go2rtc, env vars, Protect adoption, browser view |
| `cmd/bellpush/rtspserver/*` | (Phase 4) deprecate → remove once go2rtc is default |

## Risks / open questions

- **Protect ONVIF adoption (linchpin):** go2rtc's ONVIF server isn't in its explicitly-tested
  client list for UniFi Protect. Validate adoption on the UDM SE in Phase 1 before app changes.
  Fallback if it fails: keep go2rtc for browser/snapshot and use unifi-cam-proxy purely for
  Protect ingest.
- **Device contention:** ensure exactly one owner of `/dev/video0` (go2rtc). bellpush must not
  capture when `GO2RTC_URL` is set.
- **`rpicam-vid` availability/flags** vary by OS (Bullseye `libcamera`/`raspivid` vs Bookworm
  `rpicam-apps`). Confirm the exact capture command on the target image.
- **Snapshot freshness/latency** depends on H.264 keyframe (GOP) interval; tune `rpicam-vid`
  `--intra`/`-g` if snapshots are slow to resolve.
- **Browser origin/CORS** when embedding go2rtc from `:1984`; reverse-proxy if single-origin is
  preferred.
- **Cross-compilation/release:** go2rtc is shipped as its own prebuilt binary (not built by our
  Makefile), so the release/install flow must fetch it for the Pi arch.
