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
    lastPressed := time.Now()
    _ = lastPressed

    // clientOutputChannels := []chan []byte{}

    http.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
        conn, _ := upgrader.Upgrade(w, r, nil) // TODO error ignored for sake of simplicity
	
        for {
            // Read message from browser
            msgType, msg, err := conn.ReadMessage()
            if err != nil {
                return
            }

            // Print the message to the console
            fmt.Printf("%s sent: %s\n", conn.RemoteAddr(), string(msg))

            // Write message back to browser
            if err = conn.WriteMessage(msgType, msg); err != nil {
                return
            }
        }
    })

    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        http.ServeFile(w, r, "websockets.html")
    })

    http.HandleFunc("/push-button", func(w http.ResponseWriter, r *http.Request){
        lastPressed = time.Now()
    })

	fmt.Println("Starting server...")
    http.ListenAndServe("0.0.0.0:8080", nil)
}