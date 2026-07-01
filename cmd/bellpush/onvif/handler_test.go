package onvif

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stuartleeks/pi-bell/cmd/bellpush/bellpush"
)

// newFakeGo2rtc starts a fake go2rtc ONVIF server that echoes back which path
// it was called on (mimicking go2rtc exposing Device ops at /onvif/device_service
// and Media ops at a separate /onvif/media_service), and otherwise returns a
// canned GetServices response.
func newFakeGo2rtc(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = w.Write([]byte(`<soapenv:Envelope xmlns:soapenv="http://www.w3.org/2003/05/soap-envelope">` +
			`<soapenv:Body><tds:GetServicesResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl">` +
			`<tds:Service><tds:Namespace>http://www.onvif.org/ver10/device/wsdl</tds:Namespace>` +
			`<tds:XAddr>http://localhost:1984/onvif/device_service</tds:XAddr></tds:Service>` +
			`<!--fake-go2rtc-path:` + r.URL.Path + `--></tds:GetServicesResponse></soapenv:Body></soapenv:Envelope>`))
	}))
}

func TestDeviceService_ProxiesToRequestPath(t *testing.T) {
	// go2rtc exposes Media operations at a different path (/onvif/media_service)
	// than Device operations (/onvif/device_service); bellpush must proxy each
	// request to go2rtc at the same path it was received on, not hardcode
	// /onvif/device_service (see the "invalid credentials" investigation:
	// hardcoding it left Media calls 404ing against bellpush).
	go2rtc := newFakeGo2rtc(t)
	defer go2rtc.Close()

	h := NewHandler(go2rtc.URL, bellpush.NewMotionState(), "cell", "")

	reqBody := `<soapenv:Envelope xmlns:soapenv="http://www.w3.org/2003/05/soap-envelope"><soapenv:Body>` +
		`<trt:GetProfiles xmlns:trt="http://www.onvif.org/ver10/media/wsdl"/></soapenv:Body></soapenv:Envelope>`
	req := httptest.NewRequest(http.MethodPost, "/onvif/media_service", strings.NewReader(reqBody))
	req.Host = "pibell-0:8080"
	rec := httptest.NewRecorder()

	h.DeviceService(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "fake-go2rtc-path:/onvif/media_service") {
		t.Errorf("expected go2rtc to be called at /onvif/media_service, got: %s", rec.Body.String())
	}
}

func TestDeviceService_ProxiesAndInjectsEvents(t *testing.T) {
	go2rtc := newFakeGo2rtc(t)
	defer go2rtc.Close()

	h := NewHandler(go2rtc.URL, bellpush.NewMotionState(), "cell", "")

	reqBody := `<soapenv:Envelope xmlns:soapenv="http://www.w3.org/2003/05/soap-envelope"><soapenv:Body>` +
		`<tds:GetServices xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/></soapenv:Body></soapenv:Envelope>`
	req := httptest.NewRequest(http.MethodPost, "/onvif/device_service", strings.NewReader(reqBody))
	req.Host = "pibell-0:8080"
	rec := httptest.NewRecorder()

	h.DeviceService(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "localhost:1984") {
		t.Errorf("expected go2rtc host to be rewritten to bellpush host, got: %s", body)
	}
	if !strings.Contains(body, "http://pibell-0:8080/onvif/device_service") {
		t.Errorf("expected rewritten device XAddr, got: %s", body)
	}
	if !strings.Contains(body, "http://pibell-0:8080/onvif/event_service") {
		t.Errorf("expected injected events XAddr, got: %s", body)
	}
}

// pullMessagesRequestBody builds a PullMessages SOAP body with the given timeout/limit.
func pullMessagesRequestBody(timeout string, limit int) string {
	return `<soapenv:Envelope xmlns:soapenv="http://www.w3.org/2003/05/soap-envelope"><soapenv:Body>` +
		`<tev:PullMessages xmlns:tev="http://www.onvif.org/ver10/events/wsdl">` +
		`<tev:Timeout>` + timeout + `</tev:Timeout>` +
		`<tev:MessageLimit>` + strconv.Itoa(limit) + `</tev:MessageLimit>` +
		`</tev:PullMessages></soapenv:Body></soapenv:Envelope>`
}

func TestEventService_FullPullPointFlow(t *testing.T) {
	motionState := bellpush.NewMotionState()
	h := NewHandler("http://unused.invalid", motionState, "cell", "TestSource")

	// CreatePullPointSubscription
	createReq := httptest.NewRequest(http.MethodPost, "/onvif/event_service", strings.NewReader(
		`<soapenv:Envelope xmlns:soapenv="http://www.w3.org/2003/05/soap-envelope"><soapenv:Body>`+
			`<tev:CreatePullPointSubscription xmlns:tev="http://www.onvif.org/ver10/events/wsdl">`+
			`<tev:InitialTerminationTime>PT60S</tev:InitialTerminationTime>`+
			`</tev:CreatePullPointSubscription></soapenv:Body></soapenv:Envelope>`))
	createReq.Host = "pibell-0:8080"
	createRec := httptest.NewRecorder()
	h.EventService(createRec, createReq)

	if createRec.Code != http.StatusOK {
		t.Fatalf("CreatePullPointSubscription: expected 200, got %d: %s", createRec.Code, createRec.Body.String())
	}
	createBody := createRec.Body.String()
	if !strings.Contains(createBody, "SubscriptionReference") {
		t.Fatalf("expected SubscriptionReference in response: %s", createBody)
	}

	// Extract the "sub" query param from the SubscriptionReference address.
	addrStart := strings.Index(createBody, "<wsa:Address>") + len("<wsa:Address>")
	addrEnd := strings.Index(createBody, "</wsa:Address>")
	address := createBody[addrStart:addrEnd]
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatalf("failed to parse subscription address %q: %v", address, err)
	}
	subID := parsed.Query().Get("sub")
	if subID == "" {
		t.Fatalf("expected non-empty sub id in address %q", address)
	}

	// First PullMessages: should get the "Initialized" message immediately.
	pullReq := httptest.NewRequest(http.MethodPost, "/onvif/event_service?sub="+subID, strings.NewReader(pullMessagesRequestBody("PT5S", 10)))
	pullRec := httptest.NewRecorder()
	h.EventService(pullRec, pullReq)
	if pullRec.Code != http.StatusOK {
		t.Fatalf("PullMessages: expected 200, got %d: %s", pullRec.Code, pullRec.Body.String())
	}
	if !strings.Contains(pullRec.Body.String(), `PropertyOperation="Initialized"`) {
		t.Fatalf("expected Initialized message on first pull: %s", pullRec.Body.String())
	}

	// Trigger motion, then pull again in a goroutine-free synchronous manner (short poll window).
	motionState.SetActive(true)
	pullReq2 := httptest.NewRequest(http.MethodPost, "/onvif/event_service?sub="+subID, strings.NewReader(pullMessagesRequestBody("PT2S", 10)))
	pullRec2 := httptest.NewRecorder()
	h.EventService(pullRec2, pullReq2)
	if pullRec2.Code != http.StatusOK {
		t.Fatalf("PullMessages(2): expected 200, got %d: %s", pullRec2.Code, pullRec2.Body.String())
	}
	body2 := pullRec2.Body.String()
	if !strings.Contains(body2, `PropertyOperation="Changed"`) || !strings.Contains(body2, `Name="IsMotion" Value="true"`) {
		t.Fatalf("expected Changed(true) message after motion, got: %s", body2)
	}

	// Renew
	renewReq := httptest.NewRequest(http.MethodPost, "/onvif/event_service?sub="+subID, strings.NewReader(
		`<soapenv:Envelope xmlns:soapenv="http://www.w3.org/2003/05/soap-envelope"><soapenv:Body>`+
			`<wsnt:Renew xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2"><wsnt:TerminationTime>PT60S</wsnt:TerminationTime></wsnt:Renew>`+
			`</soapenv:Body></soapenv:Envelope>`))
	renewRec := httptest.NewRecorder()
	h.EventService(renewRec, renewReq)
	if renewRec.Code != http.StatusOK {
		t.Fatalf("Renew: expected 200, got %d: %s", renewRec.Code, renewRec.Body.String())
	}

	// Unsubscribe
	unsubReq := httptest.NewRequest(http.MethodPost, "/onvif/event_service?sub="+subID, strings.NewReader(
		`<soapenv:Envelope xmlns:soapenv="http://www.w3.org/2003/05/soap-envelope"><soapenv:Body>`+
			`<wsnt:Unsubscribe xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2"/></soapenv:Body></soapenv:Envelope>`))
	unsubRec := httptest.NewRecorder()
	h.EventService(unsubRec, unsubReq)
	if unsubRec.Code != http.StatusOK {
		t.Fatalf("Unsubscribe: expected 200, got %d: %s", unsubRec.Code, unsubRec.Body.String())
	}

	// A pull after unsubscribe should now fail.
	pullReq3 := httptest.NewRequest(http.MethodPost, "/onvif/event_service?sub="+subID, strings.NewReader(pullMessagesRequestBody("PT1S", 10)))
	pullRec3 := httptest.NewRecorder()
	h.EventService(pullRec3, pullReq3)
	if pullRec3.Code != http.StatusBadRequest {
		t.Fatalf("expected pull after unsubscribe to fail with 400, got %d: %s", pullRec3.Code, pullRec3.Body.String())
	}
}

func TestEventService_GetServiceCapabilitiesAndProperties(t *testing.T) {
	h := NewHandler("http://unused.invalid", bellpush.NewMotionState(), "alarm", "")

	for _, op := range []string{"GetServiceCapabilities", "GetEventProperties"} {
		reqBody := `<soapenv:Envelope xmlns:soapenv="http://www.w3.org/2003/05/soap-envelope"><soapenv:Body>` +
			`<tev:` + op + ` xmlns:tev="http://www.onvif.org/ver10/events/wsdl"/></soapenv:Body></soapenv:Envelope>`
		req := httptest.NewRequest(http.MethodPost, "/onvif/event_service", strings.NewReader(reqBody))
		rec := httptest.NewRecorder()
		h.EventService(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", op, rec.Code, rec.Body.String())
		}
	}

	// alarm mode: GetEventProperties should advertise the VideoSource/MotionAlarm topic.
	req := httptest.NewRequest(http.MethodPost, "/onvif/event_service", strings.NewReader(
		`<soapenv:Envelope xmlns:soapenv="http://www.w3.org/2003/05/soap-envelope"><soapenv:Body>`+
			`<tev:GetEventProperties xmlns:tev="http://www.onvif.org/ver10/events/wsdl"/></soapenv:Body></soapenv:Envelope>`))
	rec := httptest.NewRecorder()
	h.EventService(rec, req)
	if !strings.Contains(rec.Body.String(), "MotionAlarm") {
		t.Errorf("expected alarm topic to be advertised, got: %s", rec.Body.String())
	}
}

func TestEventService_UnsupportedOperation(t *testing.T) {
	h := NewHandler("http://unused.invalid", bellpush.NewMotionState(), "cell", "")
	req := httptest.NewRequest(http.MethodPost, "/onvif/event_service", strings.NewReader(
		`<soapenv:Envelope xmlns:soapenv="http://www.w3.org/2003/05/soap-envelope"><soapenv:Body>`+
			`<tev:SomeUnknownOperation xmlns:tev="http://www.onvif.org/ver10/events/wsdl"/></soapenv:Body></soapenv:Envelope>`))
	rec := httptest.NewRecorder()
	h.EventService(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unsupported operation, got %d", rec.Code)
	}
}
