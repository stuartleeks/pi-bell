# gobot v1 fork → gobot v2 migration

## Goal

Migrate from the pinned **`stuartleeks/gobot` fork of v1.14** to upstream
**`gobot.io/x/gobot/v2` (v2.6.0)** in order to:

1. **Enable an internal pull-down on the PIR pin (GPIO23)** to rule out a floating input as a
   cause of overnight false "Motion detected" triggers. The current sysfs-based fork has **no**
   API for configuring internal pull resistors.
2. **Remove the `replace` directive / fork entirely.** The fork exists solely for an inverted
   relay fix (hybridgroup/gobot#742, referenced at `cmd/chime/main.go:360`), which v2 supports
   natively.

## Background / why this fixes the PIR issue

Symptom: with the PIR lens physically covered (a jumper over the sensor), the bellpush still
logged "Motion detected" multiple times overnight.

The smoothing filter in `cmd/bellpush/bellpush/motion.go` is faithful — it only reports motion
when GPIO23 genuinely reads high for >50% of a 1s rolling window. So the spurious highs come from
the pin, not the logic. Likely causes: thermal drift, RF/power-supply noise, or — the one this
migration addresses — a **floating input** (no pull resistor asserted in software).

Note: BCM GPIO23 defaults to a *hardware* pull-down at power-on, so a floating pin is not the
most likely culprit, but asserting a software pull-down cleanly rules it out and is good practice.
Hardware causes (thermal/RF) would remain and are tracked separately (filter tuning via the
`PIR_*` env vars in `cmd/bellpush/main.go`).

## Capability comparison

| | Current: `stuartleeks/gobot` fork of v1.14 | `gobot.io/x/gobot/v2` v2.6.0 |
|---|---|---|
| GPIO backend | legacy **sysfs** (`/sys/class/gpio`) | modern **cdev** (`/dev/gpiochip0`) |
| Internal pull resistors | ❌ not supported | ✅ `adaptors.WithGpiosPullDown/Up(pin)` |
| Inverted relay | ❌ needs the fork (PR #742) | ✅ `gpio.WithRelayInverted()` option |
| Extras | — | hardware debounce, edge-event callbacks |

## Confirmed compatibility (verified against v2.6.0 source)

- **Pin maps are identical**: keys are physical pin numbers (`"11"`, `"16"`, `"18"`). Our
  `internal/pkg/pi` constants (e.g. `GPIO23 = "16"`) work unchanged in v2.
- **`DigitalRead(id string) (int, error)`** signature is unchanged → the `digitalReader`
  interface in `motion.go` still matches; no change to the motion sensor read loop.
- **`ButtonDriver`** still embeds `gobot.Eventer`, so `button.On(gpio.ButtonPush, handler)` and
  `gpio.ButtonRelease` continue to work with the same handler signature.
- **`raspi.NewAdaptor(opts ...interface{})`** accepts the `adaptors.With*` options and passes
  them through to the digital-pins adaptor.

## Migration steps

### 1. Dependencies (`go.mod`)

- Remove the `replace gobot.io/x/gobot => github.com/stuartleeks/gobot v1.14.1-...` line.
- Replace the `gobot.io/x/gobot v1.16.0` require with `gobot.io/x/gobot/v2 v2.6.0`.
- `go mod tidy`.

### 2. Update imports (all `gobot.io/x/gobot/...` → `gobot.io/x/gobot/v2/...`)

Files: `cmd/bellpush/bellpush/bellpush.go`, `cmd/chime/main.go`.
- `gobot.io/x/gobot/drivers/gpio`      → `gobot.io/x/gobot/v2/drivers/gpio`
- `gobot.io/x/gobot/platforms/raspi`   → `gobot.io/x/gobot/v2/platforms/raspi`
- Add `gobot.io/x/gobot/v2/platforms/adaptors` where the pull-down option is used.

### 3. bellpush: add the PIR pull-down (`cmd/bellpush/bellpush/bellpush.go`)

- `StartGpio`: `raspi.NewAdaptor()` → `raspi.NewAdaptor(adaptors.WithGpiosPullDown(pirPinNumber))`.
  (`pirPinNumber` is already the physical-pin string `"16"`.)
- Button setup and the `startMotionSensor` read loop stay as-is.

### 4. chime: convert the inverted relay + drop the fork TODO (`cmd/chime/main.go`)

- `relay = gpio.NewRelayDriver(raspberryPi, pi.GPIO18)` + `relay.Inverted = true`
  → `relay = gpio.NewRelayDriver(raspberryPi, pi.GPIO18, gpio.WithRelayInverted())`.
- Remove the `// TODO - when this PR is merged, remove the replace ...` comment referencing
  hybridgroup/gobot#742.
- `led`/`relay` `.Start()` / `.Off()` calls are unchanged.

### 5. Build & verify

- `go build ./...` and `go test ./...` (motion tests use a fake `digitalReader`, unaffected).
- `make checks` (golangci-lint) — watch for the `errcheck`/`revive` linters on any new option
  wiring.
- `make build-bellpush` / `make build-chime` cross-compile for ARM (zig) to confirm the v2 cdev
  backend builds for the Pi target.

## Deployment changes (Raspberry Pi)

The v2 default **cdev** backend accesses the GPIO **character device** (`/dev/gpiochip0`) instead
of the older, more permissive sysfs tree. The service user must be able to open it:

- Ensure the service user is in the **`gpio`** group:
  `sudo usermod -aG gpio <service-user>` (member of `gpio` grants access to `/dev/gpiochip*` via
  the udev rules shipped by Raspberry Pi OS).
- Review the systemd unit files in `scripts/` (bellpush + chime services) and the install script:
  - Confirm which user each service runs as; add it to `gpio` if not already.
  - If running as `root`, no group change is needed, but least-privilege is preferred.
- After deploying, verify: motion/button/relay still function, and check `journalctl` for any
  `/dev/gpiochip0: permission denied` errors on start.

### Fallback / de-risking

- If cdev permissions become a blocker on a given Pi, v2 can be forced back to the legacy backend
  with `raspi.NewAdaptor(adaptors.WithGpioSysfsAccess())` — but sysfs does **not** support the
  pull-down, so this is only a temporary compatibility escape hatch, not the target state.

## Out of scope (tracked elsewhere)

- Hardware false-trigger causes (thermal/RF) and PIR filter tuning via the `PIR_*` env vars
  (`cmd/bellpush/main.go`) — the pull-down only addresses the floating-input hypothesis.
