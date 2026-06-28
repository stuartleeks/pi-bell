# Migrating bellpush from systemd to k3s

## Problem Statement

The `bellpush` application currently runs as a systemd service on a Raspberry Pi. The same Pi has k3s installed with other applications. The goal is to explore what's needed to run bellpush (and potentially chime) as k3s Deployments instead of systemd services.

## Current Architecture

- **bellpush** — Go binary that detects doorbell presses via GPIO6, streams webcam frames from `/dev/video0`, and serves a WebSocket + HTTP API on port 8080. Runs as a systemd service (`pibell-bellpush.service`) as root.
- **chime** — Go binary that connects to the bellpush WebSocket, drives a relay on GPIO18 and a status LED on GPIO17. Runs as a systemd service (`pibell-chime.service`) as root.
- Both use the `gobot.io/x/gobot` library (via `periph.io`) for GPIO and are cross-compiled for `linux/arm`.
- Configuration is via environment files (`bellpush.env`, `chime.env`) and CLI flags.
- No Dockerfile or container image exists today.

## Key Considerations & Challenges

### 1. GPIO Access from Containers

This is the biggest challenge. Both binaries interact with GPIO pins through the gobot/periph.io libraries which access hardware via `/dev/gpiomem` or `/dev/mem`.

**Options:**
- **Privileged container** — Simplest: set `securityContext.privileged: true`. Grants full device access but is a broad security risk.
- **Device allowlist** — Use `securityContext.capabilities` to add `SYS_RAWIO` and mount specific device nodes (`/dev/gpiomem`, `/dev/mem`) via `hostPath` volumes. More targeted but still requires testing with periph.io's access pattern.
- **Device plugin** — Write or use a k8s device plugin that exposes GPIO as a schedulable resource. More k8s-native but significant extra work.

**Recommendation:** Start with privileged containers to prove the concept, then tighten to device allowlists.

### 2. Webcam / V4L2 Access (bellpush only)

The bellpush opens `/dev/video0` using the `go4vl` library (V4L2). This requires:
- A `hostPath` volume mount for `/dev/video0` into the container.
- The container needs permissions to access the video device (likely `privileged: true` or the `video` group).
- If the webcam device index changes (e.g. after a reboot), the path could shift. Consider mounting the full `/dev` or using a udev rule to create a stable symlink on the host.

### 3. Container Image

No Dockerfile exists. One needs to be created.

**Build considerations:**
- **CGO dependency** — `bellpush` uses `go4vl` which requires CGO. The current Makefile cross-compiles with zig (`CC="zig cc -target arm-linux-musleabihf"`). The Dockerfile could either:
  - Build natively on an ARM runner/builder (simpler CGO).
  - Use a multi-stage build with zig for cross-compilation (matches current flow).
- **Base image** — A minimal image like `alpine` or `scratch` should work, though `go4vl`'s musl linkage needs verification. Debian-slim may be safer if dynamic libs are needed.
- **Multi-arch** — Target `linux/arm/v7` (bellpush) and `linux/arm/v6` (chime). Could consolidate to `arm/v7` if the chime Pi supports it.
- **Registry** — The CI already logs into `ghcr.io`. Push container images there alongside the existing tar.gz release.

### 4. Node Affinity / Scheduling

Each pod is tied to a specific physical Pi because of its wired GPIO and camera connections:
- The bellpush pod **must** run on the Pi with the doorbell button and camera.
- Each chime pod **must** run on its respective Pi with the relay and LED.

**Approach:**
- Label each node (e.g. `pi-bell/role: bellpush`, `pi-bell/role: chime`), and use `nodeSelector` or `nodeAffinity` in the pod specs.
- Set `replicas: 1` — scaling makes no sense when each replica is bound to unique hardware.
- Consider using a **DaemonSet** with node selectors instead of a Deployment, since it naturally expresses "one pod per matching node".

### 5. Networking

The bellpush has two types of traffic:
- **HTTP** — `/ping`, `/`, `/button/*`, `/camera/latest`, `/chime/snooze`, `/chime/unsnooze`
- **WebSocket** — `/doorbell` (long-lived connection used by chimes)

The k3s cluster has **Traefik** as its ingress controller. There are three options:

- **Option A: Traefik Ingress (recommended)** — Consistent with how other services in the cluster are exposed. Create a ClusterIP Service for bellpush and an Ingress (or Traefik `IngressRoute` CRD) with a routing rule.
  - Traefik supports WebSocket natively — it detects the `Connection: Upgrade` / `Upgrade: websocket` headers and proxies the connection through. No special annotation needed.
  - **Timeout consideration:** Traefik's default transport timeout can close idle WebSocket connections. The bellpush WebSocket is not fully idle (it sends button events and the chime sends hello), but during quiet periods there may be no traffic. Configure the Traefik middleware or `IngressRoute` with an appropriate `respondingTimeouts.readTimeout` (e.g. `0` to disable) to prevent premature disconnects.
  - **Zero chime changes:** If Traefik is configured with an entrypoint on port 8080 (matching the current bellpush port), and the hostname (`pibell-1`) still resolves to the same Pi, then chimes keep connecting to `pibell-1:8080` exactly as today. The migration is completely transparent to the chime Pis.
  - **TLS (optional):** If the cluster already uses cert-manager or Traefik's ACME, WebSocket upgrades work over `wss://` too. The chime's `websocket.Dialer` would need the scheme changed from `ws://` to `wss://` — this would require a small code change to support both.

- **Option B: `hostNetwork: true`** — The bellpush pod binds directly to the host's port 8080. Also zero chime changes, but bypasses k3s networking entirely and doesn't align with how the other cluster services are managed.

- **Option C: NodePort Service** — Pick a fixed NodePort (e.g. 30080) and update `chime.env` on each chime Pi. Works but less clean than Traefik and requires chime config changes.

### 6. Security Context

- The systemd services run as `User=root`. In k3s, the pod will likely need to run as root too (for GPIO/device access).
- Use `securityContext.runAsUser: 0` explicitly.
- Long-term, investigate running as a non-root user with only the required capabilities and device group memberships.

### 7. Environment Configuration

- Currently uses `.env` files loaded by systemd (`EnvironmentFile=`).
- In k3s, use **ConfigMaps** for non-sensitive config (`BELLPUSH`, `CHIME_NAME`, `DISABLE_GPIO`, `WEBCAM_FPS`) and **Secrets** for sensitive values (`APPINSIGHTS_INSTRUMENTATIONKEY`).

### 8. Restart & Health

- systemd has `Restart=always`. In k3s, the default pod `restartPolicy: Always` provides the same behaviour.
- Add a **liveness probe** hitting `/ping` on bellpush (it already returns a simple pong response).
- Add a **readiness probe** so the Service only routes traffic when the WebSocket server is ready.
- chime doesn't expose HTTP — consider adding a health endpoint, or rely on process-level liveness (the container exiting on fatal errors).

### 9. Logging & Telemetry

- Both apps log to stdout/stderr (captured by systemd journal today). In k3s, container stdout/stderr is automatically captured by `kubectl logs` — no change needed.
- Application Insights telemetry continues to work as-is; just ensure the instrumentation key is provided via a Secret.

### 10. Build & CI Pipeline Changes

- Add a Dockerfile (or two — one per binary).
- Update CI (`ci.yml` / `release.yml`) to build and push container images to `ghcr.io`.
- Optionally keep the tar.gz release for non-k3s installs.

## Proposed Approach

### Phase 1 — Containerise bellpush

- Create a Dockerfile for bellpush (multi-stage build, cross-compile with zig or build natively on ARM).
- Test locally with `docker run --privileged --device /dev/gpiomem --device /dev/video0`.
- Verify GPIO and webcam work inside the container.

### Phase 2 — k3s manifests for bellpush

- Create a Deployment (or DaemonSet) manifest with:
  - `privileged: true` security context
  - `hostPath` volume mounts for `/dev/gpiomem` and `/dev/video0`
  - Node selector for the bellpush Pi
  - ConfigMap and Secret for environment variables
  - Liveness/readiness probes on `/ping`
- Create a Service (NodePort or LoadBalancer) exposing port 8080.
- Deploy and validate end-to-end.

### Phase 3 — Chime connectivity

- Chime Pis are **not** k3s nodes — chime continues as a systemd service.
- Expose bellpush via **Traefik Ingress** (consistent with other cluster services):
  - Create a ClusterIP Service targeting the bellpush pod on port 8080.
  - Create an Ingress or Traefik `IngressRoute` routing to the bellpush service.
  - Add a Traefik entrypoint on port 8080 so the existing chime address (`pibell-1:8080`) continues to work — **no chime-side changes needed**.
  - Configure Traefik read timeout to avoid dropping the long-lived WebSocket connection during idle periods.

### Phase 4 — CI/CD

- Update GitHub Actions to build and push container images on release.
- Optionally add a workflow step to update the k3s manifests (e.g. image tag) or use a GitOps tool like Flux/ArgoCD.

### Phase 5 — Harden

- Replace `privileged: true` with minimal capabilities and explicit device mounts.
- Investigate non-root execution with appropriate group memberships.
- Add resource requests/limits to the pod specs.

## Open Questions

- What container registry strategy? `ghcr.io` is already configured in CI — likely the natural choice.
- Is there an existing GitOps workflow for the other k3s apps, and should pi-bell follow the same pattern?
- Should the existing systemd deployment path be preserved for bellpush alongside the k3s path, or fully replaced? (Chime systemd path is kept regardless.)
- `hostNetwork: true` vs Traefik Ingress for bellpush — Traefik is consistent with the rest of the cluster but requires a chime config update and WebSocket timeout tuning. `hostNetwork` is the zero-change fallback.
