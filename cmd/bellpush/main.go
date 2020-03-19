package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/warthog618/gpiod"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// TODO - make these configurable
const ChipName string = "gpiochip0"
const ButtonPinNumber int = 6

//////////////////////// TODO - add some structure to this!! //////////////////////////////////
//////////////////////// TODO - add error handling to sample code /////////////////////////////

func setUpButtonPressListener(handler func(buttonPressed bool)) {
	disableGpioEnv := os.Getenv("DISABLE_GPIO")
	if strings.ToLower(disableGpioEnv) != "true" {
		chip, err := gpiod.NewChip(ChipName)
		if err != nil {
			panic(err)
		}
		defer chip.Close()

		line, err := chip.RequestLine(ButtonPinNumber, gpiod.WithBothEdges(func(evt gpiod.LineEvent) {
			fmt.Printf("Got event: %v\n", evt.Type)
			buttonPressed := true
			if evt.Type == gpiod.LineEventFallingEdge {
				buttonPressed = false
			}
			handler(buttonPressed)
		}))
		defer line.Close()
	}
}

func main() {
	clientOutputChannels := make(map[chan []byte]bool)
	sendButtonPressedMessage := func() {
		message := []byte(fmt.Sprintf("Button pushed - %v", time.Now()))
		for channel := range clientOutputChannels {
			channel <- message // TODO - async send?
		}
	}

	setUpButtonPressListener(func(buttonPressed bool) {
		if buttonPressed {
			sendButtonPressedMessage()
		} // TODO - handle button released
	})

	http.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil) // TODO error ignored for sake of simplicity

		outputChannel := make(chan []byte)
		clientOutputChannels[outputChannel] = true

		for {
			message := <-outputChannel

			fmt.Printf("%s: Sending %s\n", conn.RemoteAddr(), string(message))

			// Write message back to browser
			if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
				delete(clientOutputChannels, outputChannel)
				return
			}
		}
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "websockets.html")
	})

	http.HandleFunc("/push-button", func(w http.ResponseWriter, r *http.Request) {
		sendButtonPressedMessage()
	})

	fmt.Println("Starting server...")
	http.ListenAndServe("0.0.0.0:8080", nil)
}
