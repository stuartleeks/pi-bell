package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gofrs/uuid"
	"github.com/gorilla/websocket"
	"github.com/stuartleeks/pi-bell/internal/pkg/events"
	"github.com/stuartleeks/pi-bell/internal/pkg/pi"
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

var clientOutputChannels map[chan *events.ButtonEvent]bool
var telemetryClient appinsights.TelemetryClient

func uuidGen() uuid.UUID {
	id, _ := uuid.NewV4()
	return id
}
func sendButtonEvent(buttonEvent *events.ButtonEvent) {
	jsonValue, err := buttonEvent.ToJSON()
	log.Printf("ButtonEvent: %s (err: %s)\n", jsonValue, err)

	if telemetryClient != nil {
		event := appinsights.NewEventTelemetry("button-event")
		event.Properties["id"] = fmt.Sprintf("%v", buttonEvent.ID)
		event.Properties["type"] = events.TypeToString(buttonEvent.Type)
		event.Properties["source"] = buttonEvent.Source
		telemetryClient.Track(event)
		telemetryClient.Channel().Flush()
	}

	for channel := range clientOutputChannels {
		channel <- buttonEvent
	}
}

// Set up web socket endpoint for pushing doorbell notifications
func httpDoorbellNotifications(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	outputChannel := make(chan *events.ButtonEvent, 50)
	clientOutputChannels[outputChannel] = true

	for {
		buttonEvent := <-outputChannel

		message, err := buttonEvent.ToJSON()
		if err != nil {
			log.Printf("Error converting button event to JSON: %v\n", err)
			continue
		}

		// Write message back to client
		if err := conn.WriteMessage(websocket.TextMessage, []byte(message)); err != nil {
			delete(clientOutputChannels, outputChannel)
			return
		}
	}
}

// Set up homepage for testing
func httpTestPage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "./cmd/bellpush/websockets.html")
}
func httpPing(w http.ResponseWriter, r *http.Request) {
	if telemetryClient != nil {
		telemetryClient.TrackEvent("ping")
		telemetryClient.Channel().Flush()
	}
	w.Header().Add("Content-Type", "text/html")
	_, _ = w.Write([]byte("<html><body><h1>pong</h1></body></html>"))
}

// Set up endpoints to trigger doorbell (e.g. if not running on the RaspberryPi)
func httpButtonPush(w http.ResponseWriter, r *http.Request) {
	sendButtonEvent(&events.ButtonEvent{
		Type:   events.ButtonPressed,
		Source: "web",
	})
}
func httpButtonRelease(w http.ResponseWriter, r *http.Request) {
	sendButtonEvent(&events.ButtonEvent{
		ID:     uuidGen(),
		Type:   events.ButtonReleased,
		Source: "web",
	})
}
func httpButtonPushRelease(w http.ResponseWriter, r *http.Request) {
	sendButtonEvent(&events.ButtonEvent{
		ID:     uuidGen(),
		Type:   events.ButtonPressed,
		Source: "web",
	})
	time.Sleep(1 * time.Second)
	sendButtonEvent(&events.ButtonEvent{
		ID:     uuidGen(),
		Type:   events.ButtonReleased,
		Source: "web",
	})
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

	clientOutputChannels = make(map[chan *events.ButtonEvent]bool)

	// Set up Raspberry Pi button handler for bell push
	disableGpioEnv := os.Getenv("DISABLE_GPIO")
	if strings.ToLower(disableGpioEnv) != "true" {
		raspberryPi := raspi.NewAdaptor()
		defer raspberryPi.Finalize() // nolint:errcheck

		button := gpio.NewButtonDriver(raspberryPi, buttonPinNumber)
		err := button.On(gpio.ButtonPush, func(s interface{}) {
			sendButtonEvent(&events.ButtonEvent{
				ID:     uuidGen(),
				Type:   events.ButtonPressed,
				Source: "bellpush",
			})
		})
		if err != nil {
			telemetryClient.TrackException(err)
			telemetryClient.Channel().Flush()
			panic(err)
		}
		err = button.On(gpio.ButtonRelease, func(s interface{}) {
			sendButtonEvent(&events.ButtonEvent{
				ID:     uuidGen(),
				Type:   events.ButtonReleased,
				Source: "bellpush",
			})
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
	http.HandleFunc("/", httpTestPage)
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

	fmt.Println("Starting server...")
	err := http.ListenAndServe("0.0.0.0:8080", nil)
	healthTicker.Stop()
	healthTickerDone <- true
	if err != nil {
		telemetryClient.TrackException(err)
		telemetryClient.Channel().Flush()
		panic(err)
	}
}
