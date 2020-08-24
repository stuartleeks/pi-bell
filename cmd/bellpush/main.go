package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/stuartleeks/pi-bell/internal/pkg/events"
	"github.com/stuartleeks/pi-bell/internal/pkg/pi"
	"gobot.io/x/gobot/drivers/gpio"
	"gobot.io/x/gobot/platforms/raspi"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// TODO - make this configurable
const buttonPinNumber string = pi.GPIO17

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
		raspberryPi := raspi.NewAdaptor()
		defer raspberryPi.Finalize() // nolint:errcheck

		button := gpio.NewButtonDriver(raspberryPi, buttonPinNumber)
		err := button.On(gpio.ButtonPush, func(s interface{}) {
			sendButtonEvent(&events.ButtonEvent{
				Type: events.ButtonPressed,
			})
		})
		if err != nil {
			panic(err)
		}
		err = button.On(gpio.ButtonRelease, func(s interface{}) {
			sendButtonEvent(&events.ButtonEvent{
				Type: events.ButtonReleased,
			})
		})
		if err != nil {
			panic(err)
		}

		err = button.Start()
		if err != nil {
			panic(err)
		}
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

	http.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/html")
		w.Write([]byte("<html><body><h1>pong</h1></body></html>"))
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
	err := http.ListenAndServe("0.0.0.0:8080", nil)
	if err != nil {
		panic(err)
	}
}
