# Motion detection → ONVIF events for UniFi Protect

## Goal

Make the bellpush's existing **PIR motion detection** trigger **recording in UniFi Protect
(UDM SE)** by delivering motion as **ONVIF events**, so Protect records a clip when movement is
detected at the door — reusing the same PIR signal that currently fires the webhook.

No video transcoding, no extra runtime: reuse the bellpush process and the go2rtc sidecar that
already serves the camera (see `camera-onvif.md`).

## Background research

### How Protect consumes third-party motion

UniFi Protect (since **5.0.20**, Sept 2024) can record third-party ONVIF cameras on motion, but
**only via the ONVIF Events service**: the camera must advertise an Events service, let Protect
**`CreatePullPointSubscription`**, and return motion notifications from **`PullMessages`** (long
poll). The usual motion topics are:

- `tns1:RuleEngine/CellMotionDetector/Motion` (SimpleItem `IsMotion`, true/false) — most common
- `tns1:VideoSource/MotionAlarm` (SimpleItem `State`, true/false)

Protect's AI/smart detections (person/vehicle) are **not** available to third-party ONVIF cameras
without an AI Port — plain motion is the ceiling here, which is exactly what the PIR provides.

### The blocker: go2rtc has no ONVIF Events service

Verified against go2rtc source (`internal/onvif/onvif.go`, `pkg/onvif/server.go`, v1.9.x):

- The ONVIF server implements **Device + Media operations only**.
- `GetServicesResponse` / `GetCapabilitiesResponse` advertise **only** `device/wsdl` and
  `media/wsdl` — no `events/wsdl` XAddr.
- There is **no** `GetEventProperties`, `CreatePullPointSubscription`, `PullMessages`, `Renew`,
  or `Unsubscribe`.

So Protect, when adopting go2rtc, never even attempts an event subscription, and the PIR signal
(currently only driving the webhook in `cmd/bellpush/bellpush/bellpush.go:startMotionSensor`)
never reaches ONVIF. We must provide the Events service ourselves. We will **not fork go2rtc** —
the repo deliberately ships the upstream prebuilt binary (see `camera-onvif.md`, install flow).

### Options compared

| Option | "via ONVIF" | Maintained burden | Transcode | Notes |
|---|---|---|---|---|
| **bellpush fronts go2rtc + implements Events** ⟵ chosen | ✅ | self-owned, bounded SOAP | no | Pure Go, in the existing process; reuses PIR signal |
| Fork go2rtc to add Events | ✅ | own a go2rtc fork | no | Defeats "ship upstream binary" decision |
| Protect private API toggle (webhook→script flips `recordingSettings.mode`) | ❌ not ONVIF | undocumented API + stored creds | no | Brittle; not what was asked |
| unifi-cam-proxy | partial | ❌ stale | usually yes | Already rejected in `camera-onvif.md` |

**Decision:** bellpush becomes an **ONVIF augmenting proxy** in front of go2rtc and implements the
ONVIF Events (PullPoint) service, driven by the existing PIR motion handler.

## Architecture

Point **Protect at bellpush** (`:8080`) instead of go2rtc (`:1984`). bellpush proxies Device +
Media SOAP to go2rtc unchanged (so stream/snapshot URIs still resolve to go2rtc), and adds the
Events service itself.

```
PIR sensor ──► startMotionSensor ──► motionState{active,changedAt}  (in-memory, bellpush)
                       │                       │
                       └► webhook (unchanged)  └► ONVIF PullMessages

UniFi Protect ──/onvif/device_service──► bellpush ──┬─ Device/Media ops ─► proxy to go2rtc :1984
   (adopt <pi>:8080)                                ├─ GetServices/GetCapabilities ─► proxy + inject Events XAddr
                                                    └─ Events service (/onvif/event_service):
                                                         GetEventProperties, GetServiceCapabilities,
                                                         CreatePullPointSubscription, PullMessages,
                                                         Renew, Unsubscribe
go2rtc :1984 ──► RTSP/snapshot/WebRTC/HLS (unchanged; still the video source of truth)
```

**Why front go2rtc rather than have Protect talk to both:** an ONVIF camera is a single endpoint;
Protect discovers services from one `GetServices` response. Fronting lets us add the Events XAddr
to that response while leaving the video path on go2rtc.

## Configuration

Reuse existing `GO2RTC_URL` / `GO2RTC_STREAM` (already read in `cmd/bellpush/main.go` and passed
via `SetGo2rtc`). The ONVIF proxy/events feature activates when go2rtc is enabled.

| Var | Default | Meaning |
|---|---|---|
| `ONVIF_ENABLE` (proposed) | `true` when `GO2RTC_URL` set | Enable the ONVIF proxy + Events service on bellpush's HTTP port. |
| `ONVIF_MOTION_TOPIC` (proposed) | `cell` | Which motion topic to emit: `cell` (`RuleEngine/CellMotionDetector/Motion`) or `alarm` (`VideoSource/MotionAlarm`). Lets us flip during Protect validation without a rebuild. |

Protect adoption: enter **`<pi>:8080`** as the "IP address" (host:port, same gotcha documented
for go2rtc), leave bellpush ONVIF **unauthenticated**, use **dummy** creds in Protect (Protect
sends WS-Security `UsernameToken`, which we ignore — mirrors the go2rtc 1.9.4 auth note).

## Implementation phases

### Phase 1 — shared motion state (no ONVIF yet)

Files: `cmd/bellpush/bellpush/motion.go`, `cmd/bellpush/bellpush/bellpush.go`.

- Add a small concurrency-safe `motionState` (active bool + last-changed time, `sync.RWMutex` or
  atomic). Expose it from `BellPush` (getter + a way for the HTTP server to read it).
- In `startMotionSensor` and the `StartStdioReader` `"m"`/`"n"` cases, set the state on
  detected/stopped **in addition to** the existing webhook calls (don't change webhook behaviour).
- Unit-test the state transitions (table tests, no GPIO), matching `motion_test.go` style.

### Phase 2 — ONVIF Device/Media proxy + service injection

New package: `cmd/bellpush/onvif/` (handlers + minimal SOAP helpers). Wire routes in
`cmd/bellpush/httpserver/httpServer.go` (`/onvif/`).

- Read the SOAP body, detect the operation (mirror go2rtc's `GetRequestAction` regex approach).
- For Device/Media ops: **reverse-proxy** to `${GO2RTC_URL}/onvif/device_service` and return the
  response unchanged.
- For `GetServices` and `GetCapabilities`: proxy, then **inject an Events `<XAddr>`** pointing at
  `http://<host>/onvif/event_service` (and the Events capability flag) before returning.
- Keep it host-aware: rewrite `<XAddr>` hosts to bellpush's `r.Host` so Protect calls back to
  bellpush, not go2rtc.

### Phase 3 — ONVIF Events (PullPoint) service, fed by PIR

Same package. Implement on `/onvif/event_service` (and accept the subscription-addressed
operations Protect sends):

- `GetServiceCapabilities` — advertise `WSPullPointSupport=true`.
- `GetEventProperties` — advertise the configured motion topic + the message description (the
  SimpleItem name `IsMotion`/`State`).
- `CreatePullPointSubscription` — create an in-memory subscription (ID, created/expiry), return a
  `SubscriptionReference` (an addressable URL with the ID), `CurrentTime`, `TerminationTime`.
- `PullMessages` — **long poll**: honour Protect's `Timeout` (ISO-8601 duration) and
  `MessageLimit`. Block up to the timeout; return queued `tt:NotificationMessage`(s):
  - First poll of a subscription: emit current state with `PropertyOperation="Initialized"`.
  - On each PIR transition: emit `PropertyOperation="Changed"` with `IsMotion`/`State` = true/false.
  - Include `tt:Source` SimpleItem (e.g. `VideoSourceConfigurationToken`/`VideoSourceToken`) so
    Protect can map it to the stream.
- `Renew` / `Unsubscribe` — update/remove the subscription; reap expired ones.
- Drive the queue from the Phase-1 `motionState` (e.g. a broadcast/notify channel per subscription,
  or compare-on-poll if keeping it simple). Multiple subscriptions must each get the transition.

### Phase 4 — validation (cheap → expensive ladder)

Before touching Protect, validate locally with `DISABLE_GPIO=1 DISABLE_WEBCAM=1` and the stdio
`m`/`n` keys to simulate motion:

1. **GetServices injection** — POST a `GetServices` SOAP body to `http://<pi>:8080/onvif/device_service`,
   confirm an `events/wsdl` `<XAddr>` is present and points at bellpush.
2. **GetEventProperties** — curl the event service; confirm the motion topic is advertised.
3. **PullPoint round-trip** — `CreatePullPointSubscription`, then `PullMessages`; press `m`/`n`
   on stdin and confirm `Changed` messages with the right boolean appear. Use **ONVIF Device
   Manager** for a richer client check.
4. **Adopt in Protect (linchpin):** add `<pi>:8080` as a third-party ONVIF camera, dummy creds.
   Confirm live view (proxied to go2rtc) **and** that a PIR trigger produces a motion event /
   recorded clip on the Protect timeline.
5. If no clip: flip `ONVIF_MOTION_TOPIC` (`cell` ↔ `alarm`) and re-test — the topic Protect keys
   on is undocumented and must be confirmed empirically on the UDM SE.

### Phase 5 — docs & cleanup

- README: document the ONVIF motion path, adopt-at-`:8080`, `ONVIF_ENABLE` / `ONVIF_MOTION_TOPIC`,
  and the dummy-creds requirement.
- Note that Protect should now be pointed at **bellpush**, not go2rtc directly, when motion is
  wanted (go2rtc-direct still works for video-only adoption).

## Files changed

| File | Change |
|---|---|
| `cmd/bellpush/bellpush/motion.go` | Shared `motionState`; set it on detected/stopped |
| `cmd/bellpush/bellpush/bellpush.go` | Update `startMotionSensor` + stdio `m`/`n` to update state |
| `cmd/bellpush/onvif/*` | **New** — SOAP helpers, Device/Media proxy + Events (PullPoint) service |
| `cmd/bellpush/httpserver/httpServer.go` | Route `/onvif/...`; pass go2rtc URL + motion state |
| `cmd/bellpush/main.go` | Read `ONVIF_ENABLE` / `ONVIF_MOTION_TOPIC`; wire the ONVIF handler |
| `scripts/bellpush.env` | Document/optionally set the new env vars |
| `README.md` | Document the ONVIF motion path + Protect adoption |

## Risks / open questions

- **Topic Protect accepts (linchpin):** `CellMotionDetector/Motion` vs `VideoSource/MotionAlarm`
  is undocumented for Protect's third-party path. Make it switchable and validate on the UDM SE.
- **Source token matching:** the event's `Source` SimpleItem must match the video source token
  go2rtc exposes, or Protect may not associate the motion with the stream. Cross-check against
  go2rtc's `GetProfiles`/`GetVideoSources` tokens.
- **PullMessages semantics:** must honour Protect's `Timeout`/`MessageLimit` and keep the long
  poll alive; mis-handling causes Protect to drop the subscription (no motion). Reap expired subs.
- **WS-Addressing/SubscriptionReference:** Protect addresses follow-up `PullMessages`/`Renew` to
  the returned reference; the URL scheme must be reachable (bellpush host:port, plain HTTP).
- **Auth:** keep ONVIF unauthenticated (we ignore WS-Security `UsernameToken`); dummy creds in
  Protect — same constraint already proven for go2rtc 1.9.4.
- **Single ONVIF endpoint:** Protect adopts one host; ensure Device/Media proxying preserves
  go2rtc's stream/snapshot URIs (host rewrite must not break RTSP `:8554`).
