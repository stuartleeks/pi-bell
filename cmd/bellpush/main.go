package main

import (
	"bufio"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/stuartleeks/pi-bell/cmd/bellpush/bellpush"
	"github.com/stuartleeks/pi-bell/cmd/bellpush/httpserver"
	"github.com/stuartleeks/pi-bell/internal/pkg/events"
	"github.com/stuartleeks/pi-bell/internal/pkg/pi"
	"gobot.io/x/gobot/drivers/gpio"
	"gobot.io/x/gobot/platforms/raspi"

	"github.com/microsoft/ApplicationInsights-Go/appinsights"
)

// TODO - make this configurable
const buttonPinNumber string = pi.GPIO17

var telemetryClient appinsights.TelemetryClient

// // Set up homepage for testing
//
//	func httpTestPage(w http.ResponseWriter, r *http.Request) {
//		http.ServeFile(w, r, "./cmd/bellpush/websockets.html")
//	}

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

	bellpush := bellpush.NewBellPush(telemetryClient)

	// Set up Raspberry Pi button handler for bell push
	disableGpioEnv := os.Getenv("DISABLE_GPIO")
	if strings.ToLower(disableGpioEnv) != "true" {
		raspberryPi := raspi.NewAdaptor()
		defer raspberryPi.Finalize() // nolint:errcheck

		button := gpio.NewButtonDriver(raspberryPi, buttonPinNumber)
		err := button.On(gpio.ButtonPush, func(s interface{}) {
			err := bellpush.BroadcastEvent(events.NewButtonEvent(events.ButtonPressed, "bellpush"))
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
			err2 := bellpush.BroadcastEvent(events.NewButtonEvent(events.ButtonReleased, "bellpush"))
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
					err := bellpush.BroadcastEvent(events.NewButtonEvent(events.ButtonPressed, "keyboard"))
					if err != nil {
						log.Printf("Error broadcasting button pressed event: %v\n", err)
					}
				case "r": // bell release
					err := bellpush.BroadcastEvent(events.NewButtonEvent(events.ButtonReleased, "keyboard"))
					if err != nil {
						log.Printf("Error broadcasting button released event: %v\n", err)
					}
				}
			}
			log.Printf("Exiting stdio loop\n")
		}()
	}

	bellpushHTTPServer := httpserver.NewBellPushHTTPServer(bellpush, telemetryClient)

	fmt.Println("Starting server...")
	err := bellpushHTTPServer.ListenAndServe("0.0.0.0:8080")
	stdioLoop = false
	healthTicker.Stop()
	healthTickerDone <- true
	if err != nil {
		telemetryClient.TrackException(err)
		telemetryClient.Channel().Flush()
		panic(err)
	}
}
