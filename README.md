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

TODO set up and doc steps for building and running binaries rather than from source

TODO add arguments/config for pins?

## Design

### Doorbell

TODO - components and pin connections

TODO - web socket server

### Chime

TODO - components and pin connections

TODO - client connection and retries (Status LED)

## Misc

Capture TODOs as issues :-)