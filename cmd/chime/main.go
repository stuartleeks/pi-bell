package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stuartleeks/pi-bell/internal/pkg/events"
	"github.com/stuartleeks/pi-bell/internal/pkg/pi"
	"gobot.io/x/gobot/drivers/gpio"
	"gobot.io/x/gobot/platforms/raspi"
)

var addr = flag.String("addr", "localhost:8080", "http service address")

func main() {
	flag.Parse()
	address := addr

	fmt.Println("Connecting to raspberry pi ...")
	raspberryPi := raspi.NewAdaptor()
	defer raspberryPi.Finalize()

	led := gpio.NewLedDriver(raspberryPi, pi.GPIO17)

	err := led.Start()
	if err != nil {
		panic(err) // TODO - don't panic!
	}

	relay := gpio.NewRelayDriver(raspberryPi, pi.GPIO18)
	err = relay.Start()
	if err != nil {
		panic(err) // TODO - don't panic!
	}
	// Relay type is inverted to the actual relay - use Off() to trigger the chime and On() to disable
	// (and Inverted option doesn't seem to work as it always writes 0 for off and 1 for on)
	// Have opened a PR to address this: https://github.com/hybridgroup/gobot/pull/742
	relay.On()

	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, os.Interrupt)

	for {
		err := connectAndHandleEvents(interruptChan, address, led, relay)

		if err == nil {
			// handler returned so was interrupted by user
			log.Println("Exiting")
			break
		}
		log.Printf("Failed to connect: (%T) %v\n", err, err)
		for i := 0; i < 10; i++ {
			select {
			case <-interruptChan:
				return
			default:
				led.Toggle()
				time.Sleep(500 * time.Millisecond)
			}
		}
	}
}

func blinkStatusLed(statusLed *gpio.LedDriver) func() {
	statusLed.Off()
	ledStatusCancelChan := make(chan bool, 1)
	go func() {
		for {
			statusLed.On()
			time.Sleep(100 * time.Millisecond)
			statusLed.Off()

			for i := 0; i < 20; i++ {
				select {
				case <-ledStatusCancelChan:
					return
				default:
					time.Sleep(500 * time.Millisecond)
				}
			}
		}
	}()
	cancelLedBlink := func() { ledStatusCancelChan <- true }
	return cancelLedBlink
}

func connectAndHandleEvents(interruptChan <-chan os.Signal, address *string, statusLed *gpio.LedDriver, relay *gpio.RelayDriver) error {

	u := url.URL{Scheme: "ws", Host: *address, Path: "/doorbell"}
	log.Printf("connecting to %s", u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		err = fmt.Errorf("dial to %s failed: %v", u.String(), err)
		return err
	}
	defer c.Close()

	// connected to bellpush -> start the status LED blinking
	cancelLedBlink := blinkStatusLed(statusLed)
	defer cancelLedBlink()

	resultChan := make(chan error, 1)
	log.Printf("Listening...\n")
	go func() {
		for {
			messageType, buf, err := c.ReadMessage()
			if err != nil {
				log.Printf("Error reading:  (%T) %v\n", err, err) // TODO - check for websocket.CloseError and return to trigger reconnecting? (Currently panics for repeated read on failed connection in websocket code)
				var closeError *websocket.CloseError
				var opErr *net.OpError
				if errors.As(err, &closeError) ||
					errors.As(err, &opErr) {
					resultChan <- err
					return
				}
				// TODO - are there any errors here that make sense to continue?
				continue
			}
			log.Printf("Received: %v: %s\n", messageType, string(buf))
			buttonEvent, err := events.ParseButtonEventJSON(buf)
			if err != nil {
				log.Printf("Error parsing: (%T) %v\n", err, err)
				continue
			}

			switch buttonEvent.Type {
			// NOTE - logic is inverted - see notes in setup
			case events.ButtonPressed:
				log.Println("Turning relay on")
				relay.Off()
			case events.ButtonReleased:
				log.Println("Turning relay off")
				relay.On()
			default:
				log.Printf("Unhandled ButtonEventType: %v \n", buttonEvent.Type)
			}
		}
	}()

	select {
	case <-interruptChan:
		log.Println("Returning from connectAndHandleEvents - no error")
		return nil
	case err := <-resultChan:
		log.Printf("Returning from connectAndHandleEvents - error (%T): %s\n", err, err)
		return err
	}
}
