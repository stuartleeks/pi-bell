package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"

	"github.com/gorilla/websocket"
	"github.com/stuartleeks/pi-bell/internal/pkg/events"
	"github.com/stuartleeks/pi-bell/internal/pkg/gpio-components"
	"github.com/warthog618/gpiod"
)

var addr = flag.String("addr", "localhost:8080", "http service address")

const ChipName string = "gpiochip0"

func main() {
	flag.Parse()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	u := url.URL{Scheme: "ws", Host: *addr, Path: "/doorbell"}
	log.Printf("connecting to %s", u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("dial:", err) // TODO - need to think about resiliency
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

	log.Printf("Listening...\n")
	go func() {
		for {
			messageType, buf, err := c.ReadMessage()
			if err != nil {
				log.Printf("Error reading: %v\n", err)
				continue
			}
			log.Printf("Received: %v: %s\n", messageType, string(buf))
			buttonEvent, err := events.ParseButtonEventJSON(buf)
			if err != nil {
				log.Printf("Error parsing: %v\n", err)
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
				log.Printf("Unhandled ButtonEventType: \n", buttonEvent.Type)
			}
		}
	}()

	// TODO - also exit when goroutine finishes

	<-interrupt
}
