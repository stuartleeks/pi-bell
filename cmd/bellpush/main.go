package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/stuartleeks/pi-bell/internal/pkg/events"
	"github.com/stuartleeks/pi-bell/internal/pkg/gpio-components"
	"github.com/warthog618/gpiod"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// TODO - make these configurable

const ChipName string = "gpiochip0"
const ButtonPinNumber int = 6

func main() {
	clientOutputChannels := make(map[chan *events.ButtonEvent]bool)
	sendButtonEvent := func(buttonEvent *events.ButtonEvent) {
		jsonValue, err := buttonEvent.ToJSON()
		log.Printf("ButtonEvent: %s (err: %s)\n", jsonValue, err)
		for channel := range clientOutputChannels {
			channel <- buttonEvent // TODO - async send?
		}
	}

	// Set up Raspberry Pi button handler for bell push
	disableGpioEnv := os.Getenv("DISABLE_GPIO")
	if strings.ToLower(disableGpioEnv) != "true" {
		chip, err := gpiod.NewChip(ChipName)
		if err != nil {
			panic(err)
		}
		defer chip.Close()

		button, err := gpio.NewButton(chip, ButtonPinNumber, func(buttonPressed bool) {
			if buttonPressed {
				sendButtonEvent(&events.ButtonEvent{
					Type: events.ButtonPressed,
				})
			} else {
				sendButtonEvent(&events.ButtonEvent{
					Type: events.ButtonReleased,
				})
			}
		})
		if err != nil {
			panic(err)
		}
		defer button.Close()
	}

	// Set up web socket endpoint for pushing doorbell notifications
	http.HandleFunc("/doorbell", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		outputChannel := make(chan *events.ButtonEvent)
		clientOutputChannels[outputChannel] = true

		for {
			buttonEvent := <-outputChannel

			message, err := buttonEvent.ToJSON()
			if err != nil {
				log.Printf("Error converting button event to JSON: %v\n", err)
				continue
			}

			// Write message back to browser
			if err := conn.WriteMessage(websocket.TextMessage, []byte(message)); err != nil {
				delete(clientOutputChannels, outputChannel)
				return
			}
		}
	})

	// Set up homepage for testing
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./cmd/bellpush/websockets.html")
	})

	// Set up endpoint to trigger doorbell (e.g. if not running on the RaspberryPi)
	http.HandleFunc("/push-button", func(w http.ResponseWriter, r *http.Request) {
		sendButtonEvent(&events.ButtonEvent{
			Type: events.ButtonPressed,
		})
	})

	fmt.Println("Starting server...")
	http.ListenAndServe("0.0.0.0:8080", nil)
}
