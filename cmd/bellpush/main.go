package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/stuartleeks/pi-bell/cmd/bellpush/bellpush"
	"github.com/stuartleeks/pi-bell/cmd/bellpush/httpserver"

	"github.com/microsoft/ApplicationInsights-Go/appinsights"
)

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

	disableGpioEnv := os.Getenv("DISABLE_GPIO")
	disableGpio := disableGpioEnv == "true"

	disableWebcamEnv := os.Getenv("DISABLE_WEBCAM")
	disableWebcam := disableWebcamEnv == "true"
	webcamFPSEnv := os.Getenv("WEBCAM_FPS")
	webcamFPS := 2
	if webcamFPSEnv != "" {
		parsedFPS, err := strconv.Atoi(webcamFPSEnv)
		if err != nil || parsedFPS < 1 {
			fmt.Printf("Invalid WEBCAM_FPS=%q, defaulting to %d\n", webcamFPSEnv, webcamFPS)
		} else {
			webcamFPS = parsedFPS
		}
	}

	bellpush := bellpush.NewBellPush(telemetryClient)

	if !disableGpio {
		err := bellpush.StartGpio()
		if err != nil {
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
	if disableGpio {
		bellpush.StartStdioReader()
	}

	var err error
	if disableWebcam {
		err = bellpush.StartFakeCameraCapture()
	} else {
		err = bellpush.StartCameraCapture(uint32(webcamFPS))
	}
	if err != nil {
		telemetryClient.TrackException(err)
		telemetryClient.Channel().Flush()
		panic(err)
	}

	bellpushHTTPServer := httpserver.NewBellPushHTTPServer(bellpush, telemetryClient)

	fmt.Println("Starting server...")
	err = bellpushHTTPServer.ListenAndServe("0.0.0.0:8080")
	bellpush.Stop()
	healthTicker.Stop()
	healthTickerDone <- true
	if err != nil {
		telemetryClient.TrackException(err)
		telemetryClient.Channel().Flush()
		panic(err)
	}
}
