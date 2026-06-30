# Pi-Bell - A Raspberry Pi Doorbell Project

The goal of this project is ostensibly to build a doorbell and chime that are connected over our home network. The _real_ goal of this project is for me to get some hands-on time with a Raspberry Pi :-). There are commercially available systems that offer this functionality, but where is the fun in that?

## Overview

The general idea is to use a Raspberry Pi to detect when the doorbell is pressed and to have other Raspberry Pis connected be notified so that they can trigger the chimes they are attached to:

```asciiart
+----------+    +-------------+               +-------------+    +------------+
|          |    |             | Home Network  |             |    |            |
| Doorbell +----+ RaspberryPi +------+--------+ RaspberryPi +----+ Bell chime |
|          |    |             |      |        |             |    |            |
+----------+    +-------------+      |        +-------------+    +------------+
                                     |
                                     |        +-------------+    +------------+
                                     |        |             |    |            |
                                     +--------+ RaspberryPi +----+ Bell chime |
                                              |             |    |            |
                                              +-------------+    +------------+
```

Note - this repo is still currently optimised for my usage. For example the `Makefile` has commands for syncing to my Raspberry Pis :-)

## Installing binaries

There is an install.sh in the scripts folder that you can download and run (requires sudo), or if you trust random scripts on the internet you can run

```bash
wget -q -O - https://raw.githubusercontent.com/stuartleeks/pi-bell/main/scripts/install.sh | sudo bash
```

This installs the `bellpush` and `chime` binaries to `/usr/local/bin/pi-bell`

## Running as services

### bellpush

To run the bellpush as a service, run the following commands.

```bash
sudo cp /usr/local/bin/pi-bell/pibell-bellpush.service /etc/systemd/system/pibell-bellpush.service
sudo systemctl daemon-reload
sudo systemctl enable pibell-bellpush.service
```

At this point the pibell-bellpush service is installed and will start when you restart your pi.

#### Webhook notifications

The bellpush can fire an HTTP webhook on each button press to notify external systems (e.g. for mobile push notifications).

Set the `WEBHOOK_URL` environment variable to enable this:

```env
WEBHOOK_URL=http://dash-api-go:8080/push/notify
```

When set, a `POST` request with a JSON payload is sent to the URL on every button press. If `WEBHOOK_URL` is not set, webhook functionality is disabled and the bellpush behaves as before.

#### RTSP camera stream

The bellpush exposes the camera as an RTSP stream so that standard video clients (VLC, Home Assistant, NVR software, etc.) can view the feed directly.

The stream is enabled by default on port `8554`. Configure it with the `RTSP_PORT` environment variable:

```env
# Use a custom port
RTSP_PORT=9554

# Disable RTSP entirely
RTSP_PORT=0
```

Connect with any RTSP client, e.g.:

```bash
vlc rtsp://<pi-ip>:8554/camera
```

The stream uses MJPEG over RTP (RFC 2435). If the frame rate appears low, increase `WEBCAM_FPS` (default is 2).

#### go2rtc sidecar (ONVIF / WebRTC / HLS)

For low-latency browser live view and for adoption into NVR software such as **UniFi Protect**, the bellpush can delegate all camera concerns to a [go2rtc](https://github.com/AlexxIT/go2rtc) sidecar running on the same Pi. go2rtc owns the camera device (hardware H.264, no transcode) and exposes ONVIF + RTSP + WebRTC + MSE + HLS + a JPEG snapshot endpoint from a single binary.

The `install.sh` script downloads the `go2rtc` binary for the Pi's architecture alongside `bellpush`/`chime`, together with a sample `go2rtc.yaml` and a `pibell-go2rtc.service` unit.

Run go2rtc as a service:

```bash
sudo cp /usr/local/bin/pi-bell/pibell-go2rtc.service /etc/systemd/system/pibell-go2rtc.service
sudo systemctl daemon-reload
sudo systemctl enable --now pibell-go2rtc.service
```

Then point bellpush at go2rtc by setting these environment variables (in `bellpush.env`):

```env
# Base URL of the go2rtc sidecar API
GO2RTC_URL=http://localhost:1984
# go2rtc stream name (defaults to "doorbell")
GO2RTC_STREAM=doorbell
```

When `GO2RTC_URL` is set, bellpush:

- does **not** capture the camera in-process and does **not** start its built-in RTSP server (only one process may hold the V4L2 H.264 encoder, so go2rtc is the sole owner);
- proxies `GET /camera/latest` to go2rtc's `/api/frame.jpeg` snapshot, preserving the existing `image/jpeg` + `no-store` contract used by external consumers;
- renders a WebRTC live player (with automatic MSE/HLS fallback) in the web UI.

When `GO2RTC_URL` is unset, the legacy in-process camera capture and RTSP server behave exactly as before.

go2rtc's WebUI/API is on `http://<pi>:1984/`, RTSP on `:8554`, and WebRTC on `:8555`. The **ONVIF server is served on the API port** (`:1984`) at `/onvif/device_service` — there is no separate ONVIF port. Edit `/usr/local/bin/pi-bell/go2rtc.yaml` to adjust the capture command (e.g. `raspivid` on Buster, `libcamera-vid` on Bullseye, `rpicam-vid` on Bookworm), resolution, and frame rate.

**Adopting in UniFi Protect:** go2rtc does **not** respond to ONVIF WS-Discovery, so Protect won't auto-find it — add it manually as a third-party / ONVIF camera. Protect's dialog labels the field as an *IP address* but accepts `host:port`, so enter **`<pi>:1984`** (the ONVIF service is on the go2rtc API port; entering only the IP makes Protect assume port 80 and fail with a misleading "invalid credentials" error). Leave go2rtc auth unset and enter any **dummy** username/password in Protect — go2rtc 1.9.4 doesn't parse the ONVIF WS-Security token Protect sends, so configuring real `api.username`/`password` would instead cause "invalid credentials". Protect then records the RTSP stream go2rtc advertises (`:8554`).

### chime

Before continuing, edit the `/usr/local/bin/pi-bell/chime.env` to set the address of the bellpush the chime should connect to. In the example below the chime will attempt to connect to port `8080` on the `pibell-1`.

```env
BELLPUSH=pibell-1:8080
```

To run the chime as a service, run the following commands.

```bash
sudo cp /usr/local/bin/pi-bell/pibell-chime.service /etc/systemd/system/pibell-chime.service
sudo systemctl daemon-reload
sudo systemctl enable pibell-chime.service
```

At this point the pibell-chime service is installed and will start when you restart your pi.

### Troubleshooting

The commands below can be useful when troubleshooting the services.

```bash
cat /var/log/daemon.log
# or
tail -f /var/log/daemon.log

# Get logs for chime
sudo journalctl | grep chime
# Get logs for bellpush
sudo journalctl | grep bellpush
# Follow the journalctl log
sudo journalctl -fe
```

To list video devices, 

```bash
sudo apt-get install v4l-utils

v4l2-ctl --list-devices

v4l2-ctl --info -d /dev/video0
```

## Running interactively

To run the bellpush binary, run:

```bash
# skip qualified path if you've added /usr/local/bin/pi-bell to your PATH
/usr/local/bin/pi-bell/bellpush
```

Then, assuming you ran the bellpush on `my-pi-1`, run the chime using

```bash
/user/local/bin/pi-bell/chime --addr=my-pi-1:8080
```

## Running from code

To run the doorbell from code, run the following command:

```bash
make run-bellpush
```

To run the chime run the following command (note that the `DOORBELL` value needs to be set to the name of the bellpush to connect to):

```bash
DOORBELL=bellpush-pi make run-chime
```

## Design

### Bellpush

The bell push (doorbell button) part is a bell push from a standard wired doorbell connected to `+5V` and `GPIO6`.

```asciiart
                               +----------------------------------------+
                               |  Raspberry Pi                          |
                               |                                        |
                    +-------+  |           +--------------------------+ |
                  +-+ 10kΩ  +----+GND      | Web Server               | |
                  | +-------+  |           |                          | |
+---------------+ |            |           | /doorbell                | |
|               +-+--------------+GPIO 6   |    (web socket endpoint) | |
| Doorbell      |              |           |                          | |
|               +----------------+5V       |                          | |
+---------------+              |           +--------------------------+ |
                               |                                        |
                               +----------------------------------------+

```

There is a web server in the `bellpush` with a `/doorbell` endpoint for a websocker connection. When the bell push is pressed the server sends JSON event payloads to all connected clients.

Button pressed event:

```json
{
    "type": 0
}
```

Button released event:

```json
{
    "type": 1
}
```

### Chime

The chime part of the project controls the door chime. The chime is connected as to a transformer as per the instructions with the doorbell kit but with a relay in place of the bell push. The relay is connected to ground (`GND`), `+5V` and `GPIO 18`.

In addition to the chime circuit there is a status LED to indicate whether the chime is connected to the bell push. When connected the status LED blinks every 10 seconds, when not connected it blinks rapidly.

The chime app connects to the bell push and turns on the relay when it receives a button pressed event and turns it off for button released events.

```asciiart
       +--------------+    To mains power
       |              +-------------+
+------+ Transformer  |
|      |              |
|  +---+              |                           +----------------------------------------+
|  |   +--------------+                           |  Raspberry Pi                          |
|  |                                              |                                        |
|  |  +---------------+      +------------+       |           +--------------------------+ |
|  |  |               |      |            +---------+GND      | Chime app                | |
|  |  |  Door chime   |      |  Relay     |       |           |                          | |
|  |  |               |      |            +---------+GPIO 18  | Connects to web server   | |
|  +---+T1        T3+---------+N/O        |       |           | on bell push             | |
|     |               |      |            +---------+5V       |                          | |
+------+T2        T4+---------+COMMON     |       |           |                          | |
      |               |      |            |       |           +--------------------------+ |
      +---------------+      +------------+       |                                        |
                                                  |                                        |
              +-------------------------------------+GND                                   |
              |                                   |                                        |
              |   +-----------+     +-------+     |                                        |
              +---+ Resistor  +-----+  LED  +-------+GPIO 17                               |
                  | (330 ohms)|     |       |     |                                        |
                  +-----------+     +-------+     +----------------------------------------+
```

## Changelog

### 0.2.0

* Add systemd unit files for running bellpush and chime as a service
* Fix bug in status LED when attempting to connect

### 0.1.1

* Add install script

### 0.1.0

Initial release#@
