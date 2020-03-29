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

## Running the code

To run the doorbell run the following command:

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
+------------+       |           +--------------------------+ |
|            +---------+GPIO 6   | Web Server               | |
|  Doorbell  |       |           |                          | |
|            +---------+5V       | /doorbell                | |
+------------+       |           |    (web socket endpoint) | |
                     |           |                          | |
                     |           |                          | |
                     |           +--------------------------+ |
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
