package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/microsoft/ApplicationInsights-Go/appinsights"
	"github.com/microsoft/ApplicationInsights-Go/appinsights/contracts"
	"github.com/stuartleeks/pi-bell/internal/pkg/events"
	"github.com/stuartleeks/pi-bell/internal/pkg/pi"
	"github.com/stuartleeks/pi-bell/internal/pkg/timeutils"
	"gobot.io/x/gobot/drivers/gpio"
	"gobot.io/x/gobot/platforms/raspi"
)

var addr = flag.String("addr", "localhost:8080", "http service address")

var telemetryClient appinsights.TelemetryClient
var disableGpio bool
var initTime time.Time = timeutils.MustTimeParse(time.RFC3339, "1900-01-01T00:00:00Z")
var snoozeExpiry = initTime

func _log(level contracts.SeverityLevel, format string, a ...any) {
	s := fmt.Sprintf(format, a...)
	trace := appinsights.NewTraceTelemetry(s, level)
	telemetryClient.Track(trace)
	telemetryClient.Channel().Flush()
	log.Println(s)
}

func logInformation(format string, a ...any) {
	_log(appinsights.Information, format, a...)
}

//	func logWarning(format string, a ...any) {
//		_log(appinsights.Warning, format, a...)
//	}
func logError(format string, a ...any) {
	_log(appinsights.Error, format, a...)
}

// CancellableOperation represents an ongoing cancellable operation
type CancellableOperation interface {
	IsRunning() bool
	Cancel() bool
}

type safeCancellableOperation struct {
	running     bool
	innerCancel func()
}

var _ CancellableOperation = &safeCancellableOperation{}

func (o *safeCancellableOperation) IsRunning() bool {
	return o.running
}
func (o *safeCancellableOperation) Cancel() bool {
	if o.IsRunning() {
		o.running = false
		o.innerCancel()
		return true
	}
	return false
}

// NewSafeCancellableOperation returns a CancellableOperation that prevents Cancel being called multiple times
func NewSafeCancellableOperation(cancel func()) CancellableOperation {
	return &safeCancellableOperation{
		running:     true,
		innerCancel: cancel,
	}
}

func blinkStatusLed(statusLed *gpio.LedDriver, durationBetweenFlashes time.Duration) (CancellableOperation, error) {
	if statusLed == nil {
		logInformation("LED blink started")
		cancelLedBlink := func() {
			logInformation("LED blink canceled")
		}
		cancellableOperation := NewSafeCancellableOperation(cancelLedBlink)
		return cancellableOperation, nil
	}

	logInformation("LED blink started")
	err := statusLed.Off()
	if err != nil {
		err = fmt.Errorf("failed to turn led off: %v", err)
		return nil, err
	}
	ledStatusCancelChan := make(chan bool, 1)
	go func() {
		for {
			err = statusLed.On() // TODO - report errors from here so that the main loop can be restarted
			if err != nil {
				panic(err) // TODO - don't panic!
			}
			time.Sleep(100 * time.Millisecond)
			err = statusLed.Off()
			if err != nil {
				panic(err) // TODO - don't panic!
			}

			waitEnd := time.Now().Add(durationBetweenFlashes)
			for waiting := true; waiting; {
				select {
				case <-ledStatusCancelChan:
					return
				default:
					if time.Now().After(waitEnd) {
						waiting = false
						continue
					}

					time.Sleep(500 * time.Millisecond)
				}
			}
		}
	}()
	cancelLedBlink := func() {
		logInformation("LED blink canceled")
		ledStatusCancelChan <- true
	}
	cancellableOperation := NewSafeCancellableOperation(cancelLedBlink)
	return cancellableOperation, nil
}
func connectAndHandleEvents(interruptChan <-chan os.Signal, address *string, statusLed *gpio.LedDriver, relay *gpio.RelayDriver, connectingStatusBlink CancellableOperation) error {

	defer connectingStatusBlink.Cancel() // ensure we cancel the connecting status blink on error etc
	chimeName := os.Getenv("CHIME_NAME")
	if chimeName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			logError("failed to get hostname: %v", err)
			return fmt.Errorf("failed to get hostname: %v", err)
		}
		chimeName = hostname
	}

	u := url.URL{Scheme: "ws", Host: *address, Path: "/doorbell"}
	logInformation("connecting to %s", u.String())

	dialer := &websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		err = fmt.Errorf("dial to %s failed: %v", u.String(), err)
		return err
	}
	defer conn.Close()

	resultChan := make(chan error, 1)
	logInformation("Listening...")
	go func() {
		for {
			var messageType int
			var buf []byte
			messageType, buf, err = conn.ReadMessage()
			if err != nil {
				// TODO - check for websocket.CloseError and return to trigger reconnecting?
				//        (Currently panics for repeated read on failed connection in websocket code)
				log.Printf("Error reading:  (%T) %v\n", err, err)
				var closeError *websocket.CloseError
				var opErr *net.OpError
				if errors.As(err, &closeError) ||
					errors.As(err, &opErr) {
					resultChan <- err
					return
				}
				// TODO - are there any errors here that make sense to continue?
				continue
			}
			logInformation("Received: %v: %s\n", messageType, string(buf))

			var event *events.EventCommon
			event, err = events.ParseEventJSON(buf)
			if err != nil {
				logError("Error parsing event: (%T) %v\n", err, err)
				continue
			}

			shouldReturn := false
			switch event.EventType {
			case events.EventTypeButton:
				logInformation("Handling button event")
				shouldReturn = handleButtonEvent(buf, relay, resultChan)
			case events.EventTypeSnooze:
				shouldReturn = handleSnoozeEvent(buf)
			case events.EventTypeUnSnooze:
				shouldReturn = handleUnSnoozeEvent(buf)
			default:
				logError("Unhandled event type: %v\n", event.EventType)
			}
			if shouldReturn {
				return
			}
		}
	}()

	// Send hello message with hostname
	logInformation("Sending hello message")
	helloMessage := map[string]interface{}{
		"messageType": "hello",
		"senderName":  chimeName,
	}
	err = conn.WriteJSON(helloMessage)
	if err != nil {
		err = fmt.Errorf("failed to send hello message: %v", err)
		return err
	}

	// connected to bellpush -> cancel the connecting blink and working blinking
	connectingStatusBlink.Cancel()
	runningStatusBlink, err := blinkStatusLed(statusLed, 10*time.Second)
	if err != nil {
		err = fmt.Errorf("failed to set status led blinking: %v", err)
		return err
	}
	defer runningStatusBlink.Cancel()

	select {
	case <-interruptChan:
		logInformation("Returning from connectAndHandleEvents - no error")
		return nil
	case err := <-resultChan:
		logError("Returning from connectAndHandleEvents - error (%T): %s\n", err, err)
		return err
	}
}

func handleSnoozeEvent(buf []byte) bool {
	snoozeEvent, err := events.ParseSnoozeEventJSON(buf)
	if err != nil {
		logError("Error parsing: (%T) %v\n", err, err)
		return false
	}

	eventTelemetry := appinsights.NewEventTelemetry("snooze-event")
	eventTelemetry.Properties["id"] = fmt.Sprintf("%v", snoozeEvent.ID)
	eventTelemetry.Properties["snoozeExpiry"] = snoozeEvent.SnoozeExpiry.Format(time.RFC3339)
	telemetryClient.Track(eventTelemetry)
	telemetryClient.Channel().Flush()

	logInformation("Setting snooze until %s", snoozeEvent.SnoozeExpiry.Format(time.RFC3339))
	snoozeExpiry = snoozeEvent.SnoozeExpiry

	return false
}
func handleUnSnoozeEvent(buf []byte) bool {
	unsnoozeEvent, err := events.ParseUnSnoozeEventJSON(buf)
	if err != nil {
		logError("Error parsing: (%T) %v\n", err, err)
		return false
	}

	eventTelemetry := appinsights.NewEventTelemetry("unsnooze-event")
	eventTelemetry.Properties["id"] = fmt.Sprintf("%v", unsnoozeEvent.ID)
	telemetryClient.Track(eventTelemetry)
	telemetryClient.Channel().Flush()

	logInformation("Canceling snooze")
	snoozeExpiry = initTime

	return false
}
func handleButtonEvent(buf []byte, relay *gpio.RelayDriver, resultChan chan error) bool {
	buttonEvent, err := events.ParseButtonEventJSON(buf)
	if err != nil {
		logError("Error parsing: (%T) %v\n", err, err)
		return false
	}

	eventTelemetry := appinsights.NewEventTelemetry("button-event")
	eventTelemetry.Properties["id"] = fmt.Sprintf("%v", buttonEvent.ID)
	eventTelemetry.Properties["type"] = events.TypeToString(buttonEvent.ButtonEventType)
	eventTelemetry.Properties["source"] = buttonEvent.Source
	telemetryClient.Track(eventTelemetry)
	telemetryClient.Channel().Flush()

	switch buttonEvent.ButtonEventType {
	// NOTE - logic is inverted - see notes in setup
	case events.ButtonPressed:
		if snoozeExpiry.After(time.Now()) {
			logInformation("Snoozed - not turning relay on. Snooze expires at %s", snoozeExpiry.Format(time.RFC3339))
			return false
		}
		if relay == nil {
			logInformation("Relay not connected - not turning on")
			return false
		}
		logInformation("Turning relay on")
		if err := relay.On(); err != nil {
			resultChan <- err
			return true
		}
	case events.ButtonReleased:
		if relay == nil {
			logInformation("Relay not connected - not turning off")
			return false
		}
		logInformation("Turning relay off")
		if err := relay.Off(); err != nil {
			resultChan <- err
			return true
		}
	default:
		logError("Unhandled ButtonEventType: %v \n", buttonEvent.ButtonEventType)
	}

	return false
}

func main() {
	flag.Parse()
	address := addr

	key := os.Getenv("APPINSIGHTS_INSTRUMENTATIONKEY")
	telemetryConfig := appinsights.NewTelemetryConfiguration(key) // seems happy to not not error without a key!
	// Configure the maximum delay before sending queued telemetry:
	telemetryConfig.MaxBatchInterval = 2 * time.Second
	telemetryClient = appinsights.NewTelemetryClientFromConfig(telemetryConfig)
	telemetryClient.Context().Tags.Cloud().SetRole("chime")

	logInformation("chime starting")
	disableGpioEnv := os.Getenv("DISABLE_GPIO")
	disableGpio = strings.ToLower(disableGpioEnv) == "true"
	var led *gpio.LedDriver
	var relay *gpio.RelayDriver
	if !disableGpio {
		logInformation("Connecting to raspberry pi ...")
		raspberryPi := raspi.NewAdaptor()
		defer raspberryPi.Finalize() // nolint:errcheck

		led = gpio.NewLedDriver(raspberryPi, pi.GPIO17)

		err := led.Start()
		if err != nil {
			panic(err) // TODO - don't panic!
		}

		relay = gpio.NewRelayDriver(raspberryPi, pi.GPIO18)
		relay.Inverted = true
		err = relay.Start()
		if err != nil {
			panic(err) // TODO - don't panic!
		}
		// TODO - when this PR is merged, remove the `replace` in go.mod: https://github.com/hybridgroup/gobot/pull/742
		err = relay.Off()
		if err != nil {
			panic(err) // TODO - don't panic!
		}
	}
	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, os.Interrupt)

	for {
		connecting, err := blinkStatusLed(led, 1*time.Second)
		if err != nil {
			panic(err) // TODO - don't panic!
		}
		err = connectAndHandleEvents(interruptChan, address, led, relay, connecting)
		if err == nil {
			// handler returned so was interrupted by user
			logInformation("Exiting")
			break
		}

		logError("Failed to connect: (%T) %v\n", err, err)
		for i := 0; i < 10; i++ {
			select {
			case <-interruptChan:
				return
			default:
				err = led.Toggle()
				if err != nil {
					panic(err) // TODO - don't panic!
				}
				time.Sleep(500 * time.Millisecond)
			}
		}
	}
}
