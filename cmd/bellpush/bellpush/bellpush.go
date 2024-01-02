package bellpush

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"log"
	"os"
	"time"

	"github.com/microsoft/ApplicationInsights-Go/appinsights"
	"github.com/stuartleeks/pi-bell/internal/pkg/events"
	"github.com/stuartleeks/pi-bell/internal/pkg/pi"
	"github.com/vladimirvivien/go4vl/device"
	"github.com/vladimirvivien/go4vl/v4l2"
	"gobot.io/x/gobot/drivers/gpio"
	"gobot.io/x/gobot/platforms/raspi"
)

// TODO - make this configurable
const buttonPinNumber string = pi.GPIO17

type ChimeInfo struct {
	Events    chan events.Event
	SnoozeEnd time.Time
}

type BellPush struct {
	telemetryClient appinsights.TelemetryClient
	chimes          map[string]ChimeInfo
	stopProcessing  bool
	webcamFrame     []byte
}

func NewBellPush(telemetryClient appinsights.TelemetryClient) *BellPush {
	return &BellPush{
		telemetryClient: telemetryClient,
		chimes:          make(map[string]ChimeInfo),
	}
}

// Set up Raspberry Pi button handler for bell push
func (b *BellPush) StartGpio() error {

	raspberryPi := raspi.NewAdaptor()
	defer raspberryPi.Finalize() // nolint:errcheck

	button := gpio.NewButtonDriver(raspberryPi, buttonPinNumber)
	err := button.On(gpio.ButtonPush, func(s interface{}) {
		err := b.BroadcastEvent(events.NewButtonEvent(events.ButtonPressed, "bellpush"))
		if err != nil {
			log.Printf("Error broadcasting button pressed event: %v\n", err)
			b.telemetryClient.TrackException(err)
			b.telemetryClient.Channel().Flush()
		}
	})
	if err != nil {
		b.telemetryClient.TrackException(err)
		b.telemetryClient.Channel().Flush()
		return fmt.Errorf("error setting up button push handler: %w", err)
	}
	err = button.On(gpio.ButtonRelease, func(s interface{}) {
		err2 := b.BroadcastEvent(events.NewButtonEvent(events.ButtonReleased, "bellpush"))
		if err2 != nil {
			log.Printf("Error broadcasting button released event: %v\n", err)
			b.telemetryClient.TrackException(err)
			b.telemetryClient.Channel().Flush()
		}
	})
	if err != nil {
		b.telemetryClient.TrackException(err)
		b.telemetryClient.Channel().Flush()
		return fmt.Errorf("error setting up button release handler: %w", err)
	}

	err = button.Start()
	if err != nil {
		b.telemetryClient.TrackException(err)
		b.telemetryClient.Channel().Flush()
		return fmt.Errorf("error starting button driver: %w", err)
	}
	return nil
}
func (b *BellPush) StartStdioReader() {
	go func() {
		// read from stdin
		consoleReader := bufio.NewReaderSize(os.Stdin, 1)
		log.Printf("Starting stdio loop\n")
		for !b.stopProcessing {
			input, err := consoleReader.ReadByte()
			if err != nil {
				continue
			}
			char := string(input)
			log.Printf("Read char: %s\n", char)
			switch char {
			case "b": // bell push
				err := b.BroadcastEvent(events.NewButtonEvent(events.ButtonPressed, "keyboard"))
				if err != nil {
					log.Printf("Error broadcasting button pressed event: %v\n", err)
				}
			case "r": // bell release
				err := b.BroadcastEvent(events.NewButtonEvent(events.ButtonReleased, "keyboard"))
				if err != nil {
					log.Printf("Error broadcasting button released event: %v\n", err)
				}
			}
		}
		log.Printf("Exiting stdio loop\n")
	}()
}

func (b *BellPush) StartCameraCapture() error {
	devName := "/dev/video0"
	// open device
	device, err := device.Open(
		devName,
		device.WithPixFormat(v4l2.PixFormat{PixelFormat: v4l2.PixelFmtMJPEG, Width: 640, Height: 480}),
		device.WithFPS(1),
	)
	if err != nil {
		log.Fatalf("failed to open device: %s", err)
		return fmt.Errorf("failed to open device: %w", err)
	}

	// start stream
	ctx, stop := context.WithCancel(context.TODO())
	if err := device.Start(ctx); err != nil {
		log.Fatalf("failed to start stream: %s", err)
		return fmt.Errorf("failed to start stream: %w", err)
	}

	go func() {
		for frame := range device.GetOutput() {
			b.webcamFrame = frame
			if b.stopProcessing {
				break
			}
			time.Sleep(1 * time.Second)
		}

		stop()
		device.Close()
	}()
	return nil
}
func (b *BellPush) StartFakeCameraCapture() error {
	var buf bytes.Buffer
	colors := []color.Color{
		color.RGBA{100, 200, 200, 0xff},
		color.RGBA{200, 100, 200, 0xff},
		color.RGBA{200, 200, 100, 0xff},
		color.RGBA{100, 100, 200, 0xff},
		color.RGBA{100, 200, 100, 0xff},
		color.RGBA{200, 100, 100, 0xff},
	}
	colorIndex := 0

	go func() {
		for !b.stopProcessing {

			colorIndex++
			if colorIndex >= len(colors) {
				colorIndex = 0
			}
			buf.Reset()
			curentColor := colors[colorIndex]

			width := 640
			height := 480

			upLeft := image.Point{0, 0}
			lowRight := image.Point{width, height}

			img := image.NewRGBA(image.Rectangle{upLeft, lowRight})

			// Set color for each pixel.
			for x := 0; x < width; x++ {
				for y := 0; y < height; y++ {
					switch {
					case x < width/2 && y < height/2: // upper left quadrant
						img.Set(x, y, curentColor)
					case x >= width/2 && y >= height/2: // lower right quadrant
						img.Set(x, y, color.White)
					default:
						// Use zero value.
					}
				}
			}
			err := jpeg.Encode(&buf, img, nil)
			if err != nil {
				log.Printf("Error encoding fake frame: %v\n", err)
				continue
			}
			b.webcamFrame = buf.Bytes()
			time.Sleep(1 * time.Second)
		}
	}()
	return nil
}

func (b *BellPush) Stop() {
	b.stopProcessing = true
}

func (b *BellPush) GetWebcamFrame() []byte {
	return b.webcamFrame
}

func (b *BellPush) GetChimes() map[string]ChimeInfo {
	return b.chimes
}
func (b *BellPush) GetChime(name string) (ChimeInfo, bool) {
	chime, ok := b.chimes[name]
	return chime, ok
}
func (b *BellPush) SetChime(name string, chime ChimeInfo) {
	b.chimes[name] = chime
}
func (b *BellPush) RemoveChime(name string) {
	delete(b.chimes, name)
}

func (b *BellPush) BroadcastEvent(event events.Event) error {
	jsonValue, err := event.ToJSON()
	log.Printf("Event: %s (err: %s)\n", jsonValue, err)
	if err != nil {
		return err
	}

	if b.telemetryClient != nil {
		eventTelemetry := appinsights.NewEventTelemetry(event.GetType())
		for name, value := range event.GetProperties() {
			eventTelemetry.Properties[name] = value
		}
		b.telemetryClient.Track(eventTelemetry)
		b.telemetryClient.Channel().Flush()
	}

	for _, client := range b.chimes {
		client.Events <- event
	}
	return nil
}
func (b *BellPush) SendEvent(chimeName string, event events.Event) error {
	jsonValue, err := event.ToJSON()
	log.Printf("Event: %s (err: %s)\n", jsonValue, err)
	if err != nil {
		return err
	}
	chime, ok := b.chimes[chimeName]
	if !ok {
		log.Printf("Unknown chime: %q\n", chimeName)
		return fmt.Errorf("unknown chime: %q", chimeName)
	}

	if b.telemetryClient != nil {
		eventTelemetry := appinsights.NewEventTelemetry(event.GetType())
		for name, value := range event.GetProperties() {
			eventTelemetry.Properties[name] = value
		}
		eventTelemetry.Properties["chimeName"] = chimeName
		b.telemetryClient.Track(eventTelemetry)
		b.telemetryClient.Channel().Flush()
	}

	chime.Events <- event
	return nil
}
