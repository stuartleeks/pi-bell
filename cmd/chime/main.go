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
	"github.com/stuartleeks/pi-bell/internal/pkg/gpio-components"
	"github.com/warthog618/gpiod"
)

var addr = flag.String("addr", "localhost:8080", "http service address")

const ChipName string = "gpiochip0"

func main() {
	flag.Parse()
	address := addr

	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, os.Interrupt)

	for {
		err := connectAndHandleEvents(interruptChan, address)

		if err == nil {
			// handler returned so was interrupted by user
			log.Println("Exiting")
			break
		}
		log.Printf("Failed to connect: (%T) %v\n", err, err)
		for i := 0; i < 5; i++ {
			select {
			case <-interruptChan:
				return
			default:
				time.Sleep(1 * time.Second)
			}
		}
	}
}

func connectAndHandleEvents(interruptChan <-chan os.Signal, address *string) error {

	u := url.URL{Scheme: "ws", Host: *address, Path: "/doorbell"}
	log.Printf("connecting to %s", u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		err = fmt.Errorf("dial to %s failed: %v", u.String(), err)
		return err
	}
	defer c.Close()

	fmt.Println("Connecting to GPIO...")
	chip, err := gpiod.NewChip(ChipName)
	if err != nil {
		panic(err)
	}
	defer chip.Close()

	relay, err := gpio.NewRelay(chip, 18)
	if err != nil {
		panic(err)
	}
	defer relay.Close()

	resultChan := make(chan error, 1)
	log.Printf("Listening...\n")
	go func() {
		for {
			messageType, buf, err := c.ReadMessage()
			if err != nil {
				log.Printf("Error reading:  (%T) %v\n", err, err) // TODO - check for websocket.CloseError and return to trigger reconnecting? (Currently panics for repeated read on failed connection in websocket code)
				// resultChan <- err
				// return
				var closeError *websocket.CloseError
				var opErr *net.OpError
				if errors.As(err, &closeError) ||
					errors.As(err, &opErr) {
					resultChan <- err
					return
				}
				continue
			}
			log.Printf("Received: %v: %s\n", messageType, string(buf))
			buttonEvent, err := events.ParseButtonEventJSON(buf)
			if err != nil {
				log.Printf("Error parsing: (%T) %v\n", err, err)
				continue
			}

			switch buttonEvent.Type {
			case events.ButtonPressed:
				log.Println("Turning relay on")
				relay.On()
			case events.ButtonReleased:
				log.Println("Turning relay off")
				relay.Off()
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
