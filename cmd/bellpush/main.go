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
	"github.com/stuartleeks/pi-bell/cmd/bellpush/rtspserver"

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
	// sidecar: it does not capture the camera in-process and does not start the
	// built-in RTSP server (avoids contention over the single V4L2 H.264 encoder).
	go2rtcURL := os.Getenv("GO2RTC_URL")
	go2rtcStream := os.Getenv("GO2RTC_STREAM")
	if go2rtcStream == "" {
		go2rtcStream = "doorbell"
	}
	go2rtcEnabled := go2rtcURL != ""
	if go2rtcEnabled {
		fmt.Printf("go2rtc enabled: %s (stream %q) - in-process camera capture and RTSP server disabled\n", go2rtcURL, go2rtcStream)
	}

	// RTSP server (optional). Skipped entirely when go2rtc owns the camera and
	// exposes its own RTSP server.
	rtspPort := 8554
	rtspPortEnv := os.Getenv("RTSP_PORT")
	var rtsp *rtspserver.RTSPServer
	if !go2rtcEnabled {
		if rtspPortEnv == "" || rtspPortEnv == "0" {
			if rtspPortEnv == "0" {
				fmt.Println("RTSP disabled (RTSP_PORT=0)")
			} else {
				// Default: enable RTSP on port 8554
				rtsp = rtspserver.NewRTSPServer(rtspPort)
			}
		} else {
			parsed, err := strconv.Atoi(rtspPortEnv)
			if err != nil || parsed < 1 {
				fmt.Printf("Invalid RTSP_PORT=%q, defaulting to %d\n", rtspPortEnv, rtspPort)
			} else {
				rtspPort = parsed
			}
			if rtspPort > 0 {
				rtsp = rtspserver.NewRTSPServer(rtspPort)
			}
		}
	}
	if rtsp != nil {
		err := rtsp.Start()
		if err != nil {
			fmt.Printf("Failed to start RTSP server: %v\n", err)
			telemetryClient.TrackException(err)
			telemetryClient.Channel().Flush()
		} else {
			fmt.Printf("RTSP server started on port %d (rtsp://<host>:%d/camera)\n", rtspPort, rtspPort)
			bellpush.SetOnFrame(rtsp.PublishFrame)
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
		bellpushHTTPServer.SetGo2rtc(go2rtcURL, go2rtcStream)
	}

	fmt.Println("Starting server...")
	err = bellpushHTTPServer.ListenAndServe("0.0.0.0:8080")
	bellpush.Stop()
	if rtsp != nil {
		rtsp.Stop()
	}
	healthTicker.Stop()
	healthTickerDone <- true
	if err != nil {
		telemetryClient.TrackException(err)
		telemetryClient.Channel().Flush()
		panic(err)
	}
}
