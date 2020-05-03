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
wget -q -O - https://raw.githubusercontent.com/stuartleeks/pi-bell/master/scripts/install.sh | sudo bash
```

This installs the `bellpush` and `chime` binaries to `/usr/local/bin/pi-bell`

## Installing as services

### bellpush

To run the bellpush as a service, run the following commands.

```bash
sudo cp /usr/local/bin/pi-bell/pibell-bellpush.service /etc/systemd/system/pibell-bellpush.service
sudo systemctl daemon-reload
sudo systemctl enable pibell-bellpush.service
```

At this point the pibell-bellpush service is installed and will start when you restart your pi.

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

Initial release