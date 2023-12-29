package main

import (
	"bufio"
	"embed"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stuartleeks/pi-bell/internal/pkg/events"
	"github.com/stuartleeks/pi-bell/internal/pkg/pi"
	"github.com/stuartleeks/pi-bell/internal/pkg/timeutils"
	"gobot.io/x/gobot/drivers/gpio"
	"gobot.io/x/gobot/platforms/raspi"

	"github.com/microsoft/ApplicationInsights-Go/appinsights"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// TODO - make this configurable
const buttonPinNumber string = pi.GPIO17

const messageHello string = "hello"

var initTime time.Time = timeutils.MustTimeParse(time.RFC3339, "1900-01-01T00:00:00Z")

type chimeInfo struct {
	events    chan events.Event
	snoozeEnd time.Time
}

var chimes map[string]chimeInfo
var telemetryClient appinsights.TelemetryClient

func broadcastEvent(event events.Event) error {
	jsonValue, err := event.ToJSON()
	log.Printf("Event: %s (err: %s)\n", jsonValue, err)
	if err != nil {
		return err
	}

	if telemetryClient != nil {
		eventTelemetry := appinsights.NewEventTelemetry(event.GetType())
		for name, value := range event.GetProperties() {
			eventTelemetry.Properties[name] = value
		}
		telemetryClient.Track(eventTelemetry)
		telemetryClient.Channel().Flush()
	}

	for _, client := range chimes {
		client.events <- event
	}
	return nil
}
func sendEvent(chimeName string, event events.Event) error {
	jsonValue, err := event.ToJSON()
	log.Printf("Event: %s (err: %s)\n", jsonValue, err)
	if err != nil {
		return err
	}
	chime, ok := chimes[chimeName]
	if !ok {
		log.Printf("Unknown chime: %q\n", chimeName)
		return fmt.Errorf("Unknown chime: %q", chimeName)
	}

	if telemetryClient != nil {
		eventTelemetry := appinsights.NewEventTelemetry(event.GetType())
		for name, value := range event.GetProperties() {
			eventTelemetry.Properties[name] = value
		}
		eventTelemetry.Properties["chimeName"] = chimeName
		telemetryClient.Track(eventTelemetry)
		telemetryClient.Channel().Flush()
	}

	chime.events <- event
	return nil
}

// Set up web socket endpoint for pushing doorbell notifications
func httpDoorbellNotifications(w http.ResponseWriter, r *http.Request) {
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
		log.Printf("Error reading message: %v\n", err)
		return
	}
	if t != websocket.TextMessage {
		log.Printf("Unexpected message type: %d\n", t)
		return
	}
	log.Printf("Message type: %d; Message payload: %s\n", t, p)
	var dat map[string]interface{}
	if err = json.Unmarshal(p, &dat); err != nil {
		log.Printf("Error unmarshalling message: %v\n", err)
		return
	}
	messageType, ok := dat["messageType"].(string)
	if !ok {
		log.Println("No messageType in message")
	}
	if messageType != messageHello {
		log.Printf("Unexpected messageType: %s\n", messageType)
		return
	}
	senderName, ok := dat["senderName"].(string)
	if !ok {
		log.Println("No senderName in message")
		return
	}

	// Read from message channel and write back to client
	outputChannel := make(chan events.Event, 50)
	// TODO - handle existing client: send ping message to test if still connected?
	// if _, ok := clientOutputChannels[senderName]; ok {
	// 	log.Printf("Client already connected with name: %s\n", senderName)
	// 	return
	// }
	log.Printf("Client connected with name: %q\n", senderName)
	chimes[senderName] = chimeInfo{
		events:    outputChannel,
		snoozeEnd: initTime,
	}
	for {
		buttonEvent := <-outputChannel

		message, err := buttonEvent.ToJSON()
		if err != nil {
			log.Printf("Error converting button event to JSON: %v\n", err)
			continue
		}

		// Write message back to client
		if err := conn.WriteMessage(websocket.TextMessage, []byte(message)); err != nil {
			log.Printf("Error sending message to sender %q - disconnecting: %s\n", senderName, err)
			delete(chimes, senderName)
			return
		}
	}
}

// // Set up homepage for testing
//
//	func httpTestPage(w http.ResponseWriter, r *http.Request) {
//		http.ServeFile(w, r, "./cmd/bellpush/websockets.html")
//	}

//go:embed templates/*
var f embed.FS
var templates = template.Must(template.ParseFS(f, "templates/*"))

func httpHomePage(w http.ResponseWriter, _ *http.Request) {
	type chimeModel struct {
		Name         string
		SnoozeExpiry string
	}
	chimeInfos := []chimeModel{}
	for name, chime := range chimes {
		snoozeExpiry := ""
		if chime.snoozeEnd.After(time.Now()) {
			snoozeExpiry = chime.snoozeEnd.Format(time.RFC3339)
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
func httpSnooze(w http.ResponseWriter, r *http.Request) {
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

	chime, ok := chimes[name]
	if !ok {
		log.Printf("Unknown chime: %q\n", name)
		http.Error(w, fmt.Sprintf("Unknown chime: %q", name), http.StatusBadRequest)
		return
	}

	chime.snoozeEnd = time.Now().Add(duration)
	chimes[name] = chime

	err = sendEvent(name, events.NewSnoozeEvent(chime.snoozeEnd))
	if err != nil {
		log.Printf("Error sending snooze event: %v\n", err)
		http.Error(w, fmt.Sprintf("Error sending snooze event: %v", err), http.StatusInternalServerError)
		return
	}
}

func httpPing(w http.ResponseWriter, _ *http.Request) {
	if telemetryClient != nil {
		telemetryClient.TrackEvent("ping")
		telemetryClient.Channel().Flush()
	}
	w.Header().Add("Content-Type", "text/html")
	_, _ = w.Write([]byte("<html><body><h1>pong</h1></body></html>"))
}

// Set up endpoints to trigger doorbell (e.g. if not running on the RaspberryPi)
func httpButtonPush(w http.ResponseWriter, _ *http.Request) {
	err := broadcastEvent(events.NewButtonEvent(events.ButtonPressed, "web"))
	if err != nil {
		log.Printf("Error broadcasting button pressed event: %v\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
func httpButtonRelease(w http.ResponseWriter, _ *http.Request) {
	err := broadcastEvent(events.NewButtonEvent(events.ButtonReleased, "web"))
	if err != nil {
		log.Printf("Error broadcasting button released event: %v\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
func httpButtonPushRelease(w http.ResponseWriter, _ *http.Request) {
	err := broadcastEvent(events.NewButtonEvent(events.ButtonPressed, "web"))
	if err != nil {
		log.Printf("Error broadcasting button pressed event: %v\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	time.Sleep(1 * time.Second)

	err = broadcastEvent(events.NewButtonEvent(events.ButtonReleased, "web"))
	if err != nil {
		log.Printf("Error broadcasting button released event: %v\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func main() {
	flag.Parse()

	key := os.Getenv("APPINSIGHTS_INSTRUMENTATIONKEY")
	telemetryConfig := appinsights.NewTelemetryConfiguration(key) // seems happy to not not error without a key!
	// Configure the maximum delay before sending queued telemetry:
	telemetryConfig.MaxBatchInterval = 2 * time.Second
	telemetryClient = appinsights.NewTelemetryClientFromConfig(telemetryConfig)
	telemetryClient.Context().Tags.Cloud().SetRole("bellpush")

	trace := appinsights.NewTraceTelemetry("bellpush starting", appinsights.Information)
	telemetryClient.Track(trace)
	telemetryClient.Channel().Flush()

	chimes = make(map[string]chimeInfo)

	// Set up Raspberry Pi button handler for bell push
	disableGpioEnv := os.Getenv("DISABLE_GPIO")
	if strings.ToLower(disableGpioEnv) != "true" {
		raspberryPi := raspi.NewAdaptor()
		defer raspberryPi.Finalize() // nolint:errcheck

		button := gpio.NewButtonDriver(raspberryPi, buttonPinNumber)
		err := button.On(gpio.ButtonPush, func(s interface{}) {
			err := broadcastEvent(events.NewButtonEvent(events.ButtonPressed, "bellpush"))
			if err != nil {
				log.Printf("Error broadcasting button pressed event: %v\n", err)
				telemetryClient.TrackException(err)
				telemetryClient.Channel().Flush()
			}
		})
		if err != nil {
			telemetryClient.TrackException(err)
			telemetryClient.Channel().Flush()
			panic(err)
		}
		err = button.On(gpio.ButtonRelease, func(s interface{}) {
			err2 := broadcastEvent(events.NewButtonEvent(events.ButtonReleased, "bellpush"))
			if err2 != nil {
				log.Printf("Error broadcasting button released event: %v\n", err)
				telemetryClient.TrackException(err)
				telemetryClient.Channel().Flush()
			}
		})
		if err != nil {
			telemetryClient.TrackException(err)
			telemetryClient.Channel().Flush()
			panic(err)
		}

		err = button.Start()
		if err != nil {
			telemetryClient.TrackException(err)
			telemetryClient.Channel().Flush()
			panic(err)
		}
	}

	http.HandleFunc("/doorbell", httpDoorbellNotifications)
	http.HandleFunc("/ping", httpPing)
	http.HandleFunc("/", httpHomePage)
	http.HandleFunc("/chime/snooze", httpSnooze)
	http.HandleFunc("/button/push", httpButtonPush)
	http.HandleFunc("/button/release", httpButtonRelease)
	http.HandleFunc("/button/push-release", httpButtonPushRelease)

	fmt.Println("Starting health ticker...")
	healthTicker := time.NewTicker(1 * time.Minute)
	healthTickerDone := make(chan bool)
	go func() {
		for {
			select {
			case <-healthTickerDone:
				return
			case <-healthTicker.C:
				// Send health ping to show we're still alive
				telemetryClient.TrackEvent("health-ping")
				telemetryClient.Channel().Flush()
			}
		}
	}()

	// GPIO events are disabled - set up keyboard input for simulation when testing
	stdioLoop := true
	if disableGpioEnv == "true" {
		go func() {
			// read from stdin
			consoleReader := bufio.NewReaderSize(os.Stdin, 1)
			log.Printf("Starting stdio loop\n")
			for stdioLoop {
				input, err := consoleReader.ReadByte()
				if err != nil {
					continue
				}
				char := string(input)
				log.Printf("Read char: %s\n", char)
				switch char {
				case "b": // bell push
					err := broadcastEvent(events.NewButtonEvent(events.ButtonPressed, "keyboard"))
					if err != nil {
						log.Printf("Error broadcasting button pressed event: %v\n", err)
					}
				case "r": // bell release
					err := broadcastEvent(events.NewButtonEvent(events.ButtonReleased, "keyboard"))
					if err != nil {
						log.Printf("Error broadcasting button released event: %v\n", err)
					}
				}
			}
			log.Printf("Exiting stdio loop\n")
		}()
	}

	fmt.Println("Starting server...")
	err := http.ListenAndServe("0.0.0.0:8080", nil)
	stdioLoop = false
	healthTicker.Stop()
	healthTickerDone <- true
	if err != nil {
		telemetryClient.TrackException(err)
		telemetryClient.Channel().Flush()
		panic(err)
	}
}
