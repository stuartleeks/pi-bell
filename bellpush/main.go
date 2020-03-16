package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

//////////////////////// TODO - add some structure to this!! //////////////////////////////////
//////////////////////// TODO - add error handling to sample code /////////////////////////////

func main() {
	clientOutputChannels := make(map[chan []byte]bool)

	handleButtonPressed := func() {
		message := []byte(fmt.Sprintf("Button pushed - %v", time.Now()))
		for channel := range clientOutputChannels {
			channel <- message // TODO - async send?
		}
	}

	http.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil) // TODO error ignored for sake of simplicity

		outputChannel := make(chan []byte)
		clientOutputChannels[outputChannel] = true

		for {
			message := <- outputChannel

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
		handleButtonPressed()
	})

	fmt.Println("Starting server...")
	http.ListenAndServe("0.0.0.0:8080", nil)
}
