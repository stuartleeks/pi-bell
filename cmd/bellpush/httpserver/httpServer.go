package httpserver

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/microsoft/ApplicationInsights-Go/appinsights"
	"github.com/stuartleeks/pi-bell/cmd/bellpush/bellpush"
	"github.com/stuartleeks/pi-bell/internal/pkg/events"
	"github.com/stuartleeks/pi-bell/internal/pkg/timeutils"
)

const messageHello string = "hello"

var initTime time.Time = timeutils.MustTimeParse(time.RFC3339, "1900-01-01T00:00:00Z")

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

//go:embed templates/*
var f embed.FS
var templates = template.Must(template.ParseFS(f, "templates/*"))

type BellPushHTTPServer struct {
	telemetryClient appinsights.TelemetryClient
	BellPush        *bellpush.BellPush
}

func NewBellPushHTTPServer(bellPush *bellpush.BellPush, telemetryClient appinsights.TelemetryClient) *BellPushHTTPServer {
	return &BellPushHTTPServer{
		telemetryClient: telemetryClient,
		BellPush:        bellPush,
	}
}

func (b *BellPushHTTPServer) httpHomePage(w http.ResponseWriter, _ *http.Request) {
	type chimeModel struct {
		Name         string
		SnoozeExpiry string
	}
	chimeInfos := []chimeModel{}
	for name, chime := range b.BellPush.GetChimes() {
		snoozeExpiry := ""
		if chime.SnoozeEnd.After(time.Now()) {
			snoozeExpiry = chime.SnoozeEnd.Format(time.RFC3339)
		}
		c := chimeModel{
			Name:         name,
			SnoozeExpiry: snoozeExpiry,
		}
		chimeInfos = append(chimeInfos, c)
	}
	if err := templates.ExecuteTemplate(w, "index.html", map[string]interface{}{
		"Title":  "Home Page",
		"Chimes": chimeInfos,
	}); err != nil {
		log.Printf("Error executing template: %v\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
func (b *BellPushHTTPServer) httpSnooze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		log.Printf("Invalid method: %s\n", r.Method)
		http.Error(w, "Invalid method", http.StatusMethodNotAllowed)
		return
	}

	name := r.URL.Query().Get(("name"))
	if name == "" {
		log.Printf("Missing name\n")
		http.Error(w, "Missing name", http.StatusBadRequest)
		return
	}
	durationString := r.URL.Query().Get(("duration"))
	if durationString == "" {
		log.Printf("Missing duration\n")
		http.Error(w, "Missing duration", http.StatusBadRequest)
		return
	}
	duration, err := time.ParseDuration(durationString)
	if err != nil {
		log.Printf("Invalid duration: %v\n", err)
		http.Error(w, fmt.Sprintf("Invalid duration: %v", err), http.StatusBadRequest)
		return
	}

	log.Printf("Snoozing chime %q for %f minutes\n", name, duration.Minutes())

	chime, ok := b.BellPush.GetChime(name)
	if !ok {
		log.Printf("Unknown chime: %q\n", name)
		http.Error(w, fmt.Sprintf("Unknown chime: %q", name), http.StatusBadRequest)
		return
	}

	chime.SnoozeEnd = time.Now().Add(duration)
	b.BellPush.SetChime(name, chime)

	err = b.BellPush.SendEvent(name, events.NewSnoozeEvent(chime.SnoozeEnd))
	if err != nil {
		log.Printf("Error sending snooze event: %v\n", err)
		http.Error(w, fmt.Sprintf("Error sending snooze event: %v", err), http.StatusInternalServerError)
		return
	}
}
func (b *BellPushHTTPServer) httpUnSnooze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		log.Printf("Invalid method: %s\n", r.Method)
		http.Error(w, "Invalid method", http.StatusMethodNotAllowed)
		return
	}

	name := r.URL.Query().Get(("name"))
	if name == "" {
		log.Printf("Missing name\n")
		http.Error(w, "Missing name", http.StatusBadRequest)
		return
	}

	log.Printf("UnSnoozing chime %q\n", name)

	chime, ok := b.BellPush.GetChime(name)
	if !ok {
		log.Printf("Unknown chime: %q\n", name)
		http.Error(w, fmt.Sprintf("Unknown chime: %q", name), http.StatusBadRequest)
		return
	}

	chime.SnoozeEnd = initTime
	b.BellPush.SetChime(name, chime)

	err := b.BellPush.SendEvent(name, events.NewUnSnoozeEvent())
	if err != nil {
		log.Printf("Error sending unsnooze event: %v\n", err)
		http.Error(w, fmt.Sprintf("Error sending unsnooze event: %v", err), http.StatusInternalServerError)
		return
	}
}

func (b *BellPushHTTPServer) httpPing(w http.ResponseWriter, _ *http.Request) {
	if b.telemetryClient != nil {
		b.telemetryClient.TrackEvent("ping")
		b.telemetryClient.Channel().Flush()
	}
	w.Header().Add("Content-Type", "text/html")
	_, _ = w.Write([]byte("<html><body><h1>pong</h1></body></html>"))
}

// Set up endpoints to trigger doorbell (e.g. if not running on the RaspberryPi)
func (b *BellPushHTTPServer) httpButtonPush(w http.ResponseWriter, _ *http.Request) {
	err := b.BellPush.BroadcastEvent(events.NewButtonEvent(events.ButtonPressed, "web"))
	if err != nil {
		log.Printf("Error broadcasting button pressed event: %v\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
func (b *BellPushHTTPServer) httpButtonRelease(w http.ResponseWriter, _ *http.Request) {
	err := b.BellPush.BroadcastEvent(events.NewButtonEvent(events.ButtonReleased, "web"))
	if err != nil {
		log.Printf("Error broadcasting button released event: %v\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
func (b *BellPushHTTPServer) httpButtonPushRelease(w http.ResponseWriter, _ *http.Request) {
	err := b.BellPush.BroadcastEvent(events.NewButtonEvent(events.ButtonPressed, "web"))
	if err != nil {
		log.Printf("Error broadcasting button pressed event: %v\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	time.Sleep(1 * time.Second)

	err = b.BellPush.BroadcastEvent(events.NewButtonEvent(events.ButtonReleased, "web"))
	if err != nil {
		log.Printf("Error broadcasting button released event: %v\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

var connectCounter int32

// Set up web socket endpoint for pushing doorbell notifications
func (b *BellPushHTTPServer) httpDoorbellNotifications(w http.ResponseWriter, r *http.Request) {
	connectID := atomic.AddInt32(&connectCounter, 1)
	// Upgrade to websocket connection
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	defer conn.Close()

	// Read "hello" message from client
	t, p, err := conn.ReadMessage()
	if err != nil {
		log.Printf("%d:Error reading message: %v\n", connectID, err)
		return
	}
	if t != websocket.TextMessage {
		log.Printf("%d:Unexpected message type: %d\n", connectID, t)
		return
	}
	log.Printf("%d:Message type: %d; Message payload: %s\n", connectID, t, p)
	var dat map[string]interface{}
	if err = json.Unmarshal(p, &dat); err != nil {
		log.Printf("%d:Error unmarshalling message: %v\n", connectID, err)
		return
	}
	messageType, ok := dat["messageType"].(string)
	if !ok {
		log.Printf("%d:No messageType in message\n", connectID)
	}
	if messageType != messageHello {
		log.Printf("%d:Unexpected messageType: %s\n", connectID, messageType)
		return
	}
	senderName, ok := dat["senderName"].(string)
	if !ok {
		log.Printf("%d:No senderName in message\n", connectID)
		return
	}

	// Read from message channel and write back to client
	outputChannel := make(chan events.Event, 50)
	// TODO - handle existing client: send ping message to test if still connected?
	// if _, ok := clientOutputChannels[senderName]; ok {
	// 	log.Printf("Client already connected with name: %s\n", senderName)
	// 	return
	// }
	var chime bellpush.ChimeInfo
	sendSnoozeEvent := false
	if chime, ok = b.BellPush.GetChime(senderName); ok {
		// Send stop processing event to existing client loop (before replacing with new loop)
		chime.Events <- events.NewStopProcessingEvent()
		chime.Events = outputChannel // replace with new channel for new loop
		sendSnoozeEvent = chime.SnoozeEnd.After(time.Now())
		log.Printf("%d:Existing client with name %q. SnoozeEnd: %s, sendSnoozeEvent: %v\n", connectID, senderName, chime.SnoozeEnd.Format(time.RFC3339), sendSnoozeEvent)
	} else {
		chime = bellpush.ChimeInfo{
			Events:    outputChannel,
			SnoozeEnd: initTime,
		}
	}

	log.Printf("%d:Client connected with name: %q\n", connectID, senderName)
	b.BellPush.SetChime(senderName, chime)

	if sendSnoozeEvent {
		err = b.BellPush.SendEvent(senderName, events.NewSnoozeEvent(chime.SnoozeEnd))
		if err != nil {
			log.Printf("%d:Error sending snooze event: %v\n", connectID, err)
			return
		}
	}

	// set up send loop for client
	for {
		event := <-outputChannel
		if event.GetType() == events.EventTypeStopProcessing {
			log.Printf("%d:Received StopProcessingEvent - exiting\n", connectID)
			break
		}

		message, err := event.ToJSON()
		if err != nil {
			log.Printf("%d:Error converting button event to JSON: %v\n", connectID, err)
			continue
		}
		log.Printf("*** %d:Sending message (%q): %s\n", connectID, senderName, message)

		// Write message back to client
		if err := conn.WriteMessage(websocket.TextMessage, []byte(message)); err != nil {
			log.Printf("%d:***Error sending message to sender %q - disconnecting: %s\n", connectID, senderName, err)
			b.BellPush.RemoveChime(senderName)
			return
		}
	}
}

func (b *BellPushHTTPServer) ListenAndServe(addr string) error {
	http.HandleFunc("/doorbell", b.httpDoorbellNotifications)
	http.HandleFunc("/ping", b.httpPing)
	http.HandleFunc("/", b.httpHomePage)
	http.HandleFunc("/chime/snooze", b.httpSnooze)
	http.HandleFunc("/chime/unsnooze", b.httpUnSnooze)
	http.HandleFunc("/button/push", b.httpButtonPush)
	http.HandleFunc("/button/release", b.httpButtonRelease)
	http.HandleFunc("/button/push-release", b.httpButtonPushRelease)

	return http.ListenAndServe(addr, nil)
}
