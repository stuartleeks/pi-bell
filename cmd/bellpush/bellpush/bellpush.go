package bellpush

import (
	"fmt"
	"log"
	"time"

	"github.com/microsoft/ApplicationInsights-Go/appinsights"
	"github.com/stuartleeks/pi-bell/internal/pkg/events"
)

type ChimeInfo struct {
	Events    chan events.Event
	SnoozeEnd time.Time
}

type BellPush struct {
	telemetryClient appinsights.TelemetryClient
	chimes          map[string]ChimeInfo
}

func NewBellPush(telemetryClient appinsights.TelemetryClient) *BellPush {
	return &BellPush{
		telemetryClient: telemetryClient,
		chimes:          make(map[string]ChimeInfo),
	}
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
