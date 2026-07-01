// Package onvif implements just enough of ONVIF to let UniFi Protect (and
// similar NVR software) record motion detected by the bellpush's PIR sensor.
//
// bellpush acts as an ONVIF "augmenting proxy" in front of the go2rtc sidecar
// (see .plans/motion-onvif.md): Device and Media operations are reverse
// proxied to go2rtc unchanged, GetServices/GetCapabilities responses are
// augmented with an Events service entry pointing back at bellpush, and the
// ONVIF Events (PullPoint) service itself - GetServiceCapabilities,
// GetEventProperties, CreatePullPointSubscription, PullMessages, Renew,
// Unsubscribe - is implemented here, fed by the shared bellpush.MotionState.
package onvif

import (
	"io"
	"log"
	"net/http"
	"time"

	"github.com/stuartleeks/pi-bell/cmd/bellpush/bellpush"
)

// defaultSourceToken is used as the ONVIF Source SimpleItem value on motion
// notifications when none is configured. Protect does not appear to require
// this to match a real go2rtc video source token for basic motion recording,
// but the plan flags it as an open question to validate against the real NVR.
const defaultSourceToken = "VideoSourceToken"

// Handler serves the ONVIF Device/Media proxy and Events (PullPoint) service
// used to drive UniFi Protect motion recording from the bellpush's PIR sensor.
type Handler struct {
	go2rtcURL   string
	httpClient  *http.Client
	manager     *Manager
	motionState *bellpush.MotionState
	sourceToken string
}

// NewHandler creates a Handler. go2rtcURL is the go2rtc sidecar base URL as
// reachable from bellpush itself (e.g. http://localhost:1984). topicMode
// selects the motion topic advertised/emitted ("cell" or "alarm", see
// ONVIF_MOTION_TOPIC in the README). sourceToken, if empty, defaults to
// defaultSourceToken.
func NewHandler(go2rtcURL string, motionState *bellpush.MotionState, topicMode string, sourceToken string) *Handler {
	if sourceToken == "" {
		sourceToken = defaultSourceToken
	}
	return &Handler{
		go2rtcURL:   go2rtcURL,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		manager:     NewManager(motionState, topicMode),
		motionState: motionState,
		sourceToken: sourceToken,
	}
}

// DeviceService handles both the /onvif/device_service and /onvif/media_service
// endpoints: go2rtc exposes Device operations at the former and Media
// operations at the latter (see the Device/Media XAddr entries in
// GetCapabilities), and both are reverse-proxied to go2rtc unchanged - at
// whichever of the two paths the incoming request used - with
// GetServices/GetCapabilities responses augmented to advertise the Events
// service implemented by EventService.
func (h *Handler) DeviceService(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	operation := detectOperation(body)
	h.proxyDeviceService(w, r, body, operation)
}

// readBody reads and returns the request body, writing a SOAP fault and
// returning ok=false on error.
func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid method", http.StatusMethodNotAllowed)
		return nil, false
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("onvif: error reading request body: %v\n", err)
		writeSOAPFault(w, http.StatusBadRequest, "Sender", "error reading request body")
		return nil, false
	}
	return body, true
}
