package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/stuartleeks/pi-bell/cmd/bellpush/bellpush"
	"github.com/stuartleeks/pi-bell/cmd/bellpush/httpserver"

	"github.com/microsoft/ApplicationInsights-Go/appinsights"
)

var telemetryClient appinsights.TelemetryClient

// parseMotionConfig builds the PIR motion sensor configuration from environment
// variables, leaving any unset/invalid value as zero so the bellpush package
// applies its default. Tunables:
//   - PIR_QUEUE_LEN: number of samples averaged together
//   - PIR_SAMPLE_RATE_HZ: samples per second
//   - PIR_THRESHOLD: fraction (0-1) of high samples required for motion
//   - PIR_HOLD_TIME_SECONDS: quiet time before motion is reported as stopped
func parseMotionConfig() bellpush.MotionConfig {
	var config bellpush.MotionConfig

	if v := os.Getenv("PIR_QUEUE_LEN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			config.QueueLen = n
		} else {
			fmt.Printf("Invalid PIR_QUEUE_LEN=%q, using default\n", v)
		}
	}
	if v := os.Getenv("PIR_SAMPLE_RATE_HZ"); v != "" {
		if hz, err := strconv.Atoi(v); err == nil && hz >= 1 {
			config.SampleRate = time.Second / time.Duration(hz)
		} else {
			fmt.Printf("Invalid PIR_SAMPLE_RATE_HZ=%q, using default\n", v)
		}
	}
	if v := os.Getenv("PIR_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f < 1 {
			config.Threshold = f
		} else {
			fmt.Printf("Invalid PIR_THRESHOLD=%q, using default\n", v)
		}
	}
	if v := os.Getenv("PIR_HOLD_TIME_SECONDS"); v != "" {
		if s, err := strconv.Atoi(v); err == nil && s >= 0 {
			config.HoldTime = time.Duration(s) * time.Second
		} else {
			fmt.Printf("Invalid PIR_HOLD_TIME_SECONDS=%q, using default\n", v)
		}
	}

	return config
}

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

	webhookURL := os.Getenv("WEBHOOK_URL")
	if webhookURL != "" {
		fmt.Printf("Webhook enabled: %s\n", webhookURL)
	} else {
		fmt.Println("Webhook disabled (WEBHOOK_URL not set)")
	}
	webhook := bellpush.NewWebhookNotifier(webhookURL, telemetryClient)

	// PIR motion sensor tuning (optional). Defaults are applied for any unset
	// or invalid values inside the bellpush package.
	motionConfig := parseMotionConfig()

	bellpush := bellpush.NewBellPush(telemetryClient, webhook)
	bellpush.SetMotionConfig(motionConfig)

	// go2rtc sidecar delegation (optional).
	// When GO2RTC_URL is set, bellpush delegates all camera concerns to the go2rtc
	// sidecar: it does not capture the camera in-process (avoids contention over
	// the single V4L2 H.264 encoder).
	// GO2RTC_URL must be reachable from bellpush itself (e.g. http://localhost:1984 when
	// go2rtc runs alongside bellpush on the same Pi) and is used for the /camera/latest
	// snapshot proxy. GO2RTC_PUBLIC_URL must be reachable from the viewer's browser (e.g.
	// http://pibell-0:1984) and is embedded into the served HTML for the live view; if
	// unset, it defaults to GO2RTC_URL, which only works when that address also resolves
	// correctly from the browser (i.e. it is not "localhost").
	go2rtcURL := os.Getenv("GO2RTC_URL")
	go2rtcPublicURL := os.Getenv("GO2RTC_PUBLIC_URL")
	go2rtcStream := os.Getenv("GO2RTC_STREAM")
	if go2rtcStream == "" {
		go2rtcStream = "doorbell"
	}
	go2rtcEnabled := go2rtcURL != ""
	if go2rtcEnabled {
		effectivePublicURL := go2rtcPublicURL
		if effectivePublicURL == "" {
			effectivePublicURL = go2rtcURL
		}
		fmt.Printf("go2rtc enabled: GO2RTC_URL=%q GO2RTC_PUBLIC_URL=%q (effective public URL: %q, stream %q) - in-process camera capture disabled\n", go2rtcURL, go2rtcPublicURL, effectivePublicURL, go2rtcStream)
		if effectivePublicURL == go2rtcURL && (go2rtcURL == "http://localhost:1984" || strings.Contains(go2rtcURL, "://localhost") || strings.Contains(go2rtcURL, "://127.0.0.1")) {
			fmt.Println("WARNING: GO2RTC_PUBLIC_URL is not set and GO2RTC_URL uses localhost/127.0.0.1 - the browser live view will not work remotely. Set GO2RTC_PUBLIC_URL to an address reachable from the viewer's browser (e.g. http://pibell-0:1984).")
		}
	}

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
	if !go2rtcEnabled {
		// go2rtc, when enabled, owns the camera; otherwise capture in-process.
		if disableWebcam {
			err = bellpush.StartFakeCameraCapture()
		} else {
			err = bellpush.StartCameraCapture(uint32(webcamFPS))
		}
	}
	if err != nil {
		telemetryClient.TrackException(err)
		telemetryClient.Channel().Flush()
		panic(err)
	}

	bellpushHTTPServer := httpserver.NewBellPushHTTPServer(bellpush, telemetryClient)
	if go2rtcEnabled {
		bellpushHTTPServer.SetGo2rtc(go2rtcURL, go2rtcPublicURL, go2rtcStream)

		// ONVIF Events (PullPoint) support for UniFi Protect motion recording.
		// Defaults to enabled whenever go2rtc is enabled (bellpush fronts go2rtc's
		// Device/Media ONVIF services and augments them with an Events service fed
		// by the PIR sensor - see .plans/motion-onvif.md). Set ONVIF_ENABLE=false to
		// opt out (e.g. when Protect is adopting go2rtc directly for video-only).
		onvifEnabled := go2rtcEnabled
		if v := os.Getenv("ONVIF_ENABLE"); v != "" {
			onvifEnabled = v == "true"
		}
		if onvifEnabled {
			onvifMotionTopic := os.Getenv("ONVIF_MOTION_TOPIC")
			if onvifMotionTopic != "cell" && onvifMotionTopic != "alarm" {
				if onvifMotionTopic != "" {
					fmt.Printf("Invalid ONVIF_MOTION_TOPIC=%q, defaulting to \"cell\"\n", onvifMotionTopic)
				}
				onvifMotionTopic = "cell"
			}
			bellpushHTTPServer.SetOnvif(go2rtcURL, onvifMotionTopic)
		} else {
			fmt.Println("ONVIF disabled (ONVIF_ENABLE=false)")
		}
	}

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
