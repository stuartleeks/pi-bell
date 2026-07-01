package bellpush

import (
	"bytes"
	"log"
	"net/http"
	"time"

	"github.com/microsoft/ApplicationInsights-Go/appinsights"
)

var webhookPayload = []byte(`{"type":"doorbell","title":"Doorbell","body":"Someone is at the door","url":"/doorbell/camera"}`)

var motionDetectedPayload = []byte(`{"type":"motion","title":"Motion detected","body":"Movement detected at the door","url":"/doorbell/camera"}`)

var motionStoppedPayload = []byte(`{"type":"motion","title":"Motion stopped","body":"Movement stopped at the door","url":"/doorbell/camera"}`)

// WebhookNotifier sends a fire-and-forget HTTP POST to a configured URL.
// A nil *WebhookNotifier is safe to call; all methods are no-ops.
type WebhookNotifier struct {
	url             string
	client          *http.Client
	telemetryClient appinsights.TelemetryClient
}

// NewWebhookNotifier creates a WebhookNotifier for the given URL.
// Returns nil if url is empty, making the webhook a no-op.
func NewWebhookNotifier(url string, telemetryClient appinsights.TelemetryClient) *WebhookNotifier {
	if url == "" {
		return nil
	}
	return &WebhookNotifier{
		url: url,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
		telemetryClient: telemetryClient,
	}
}

// Notify sends the doorbell webhook POST. Safe to call on a nil receiver.
func (w *WebhookNotifier) Notify() {
	w.notify(webhookPayload)
}

// NotifyMotionDetected sends a motion-detected webhook POST. Safe to call on a nil receiver.
func (w *WebhookNotifier) NotifyMotionDetected() {
	w.notify(motionDetectedPayload)
}

// NotifyMotionStopped sends a motion-stopped webhook POST. Safe to call on a nil receiver.
func (w *WebhookNotifier) NotifyMotionStopped() {
	w.notify(motionStoppedPayload)
}

// notify sends the given payload as an HTTP POST. Safe to call on a nil receiver.
func (w *WebhookNotifier) notify(payload []byte) {
	if w == nil {
		return
	}

	log.Printf("Firing webhook to %s\n", w.url)

	resp, err := w.client.Post(w.url, "application/json", bytes.NewReader(payload))
	if err != nil {
		log.Printf("Webhook error: %v\n", err)
		w.trackException(err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("Webhook returned status %d\n", resp.StatusCode)
		if w.telemetryClient != nil {
			event := appinsights.NewEventTelemetry("webhook-error")
			event.Properties["url"] = w.url
			event.Properties["statusCode"] = resp.Status
			w.telemetryClient.Track(event)
			w.telemetryClient.Channel().Flush()
		}
	}
}

func (w *WebhookNotifier) trackException(err error) {
	if w.telemetryClient != nil {
		w.telemetryClient.TrackException(err)
		w.telemetryClient.Channel().Flush()
	}
}
