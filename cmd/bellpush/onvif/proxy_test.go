package onvif

import (
	"strings"
	"testing"
)

func TestRewriteXAddrHost(t *testing.T) {
	body := []byte(`<tds:XAddr>http://localhost:1984/onvif/device_service</tds:XAddr>` +
		`<trt:XAddr>http://localhost:1984/onvif/device_service</trt:XAddr>`)

	got := string(rewriteXAddrHost(body, "pibell-0:8080"))

	if strings.Contains(got, "localhost:1984") {
		t.Errorf("expected localhost:1984 to be rewritten, got: %s", got)
	}
	wantCount := strings.Count(got, "http://pibell-0:8080/onvif/device_service")
	if wantCount != 2 {
		t.Errorf("expected 2 rewritten XAddr entries, got %d in: %s", wantCount, got)
	}
}

func TestInsertBeforeLastClosingTag(t *testing.T) {
	body := []byte(`<tds:GetServicesResponse><tds:Service>x</tds:Service></tds:GetServicesResponse>`)
	out, ok := insertBeforeLastClosingTag(body, "GetServicesResponse", "<tds:Service>INJECTED</tds:Service>")
	if !ok {
		t.Fatalf("expected closing tag to be found")
	}
	got := string(out)
	if !strings.Contains(got, "INJECTED</tds:Service></tds:GetServicesResponse>") {
		t.Errorf("expected injected fragment right before closing tag, got: %s", got)
	}
}

func TestInsertBeforeLastClosingTag_NotFound(t *testing.T) {
	body := []byte(`<foo>bar</foo>`)
	out, ok := insertBeforeLastClosingTag(body, "GetServicesResponse", "<x/>")
	if ok {
		t.Fatalf("expected not found")
	}
	if string(out) != string(body) {
		t.Errorf("expected body unchanged when tag not found")
	}
}

func TestInjectEventsService_GetServices(t *testing.T) {
	body := []byte(`<soapenv:Envelope xmlns:soapenv="http://www.w3.org/2003/05/soap-envelope"><soapenv:Body>` +
		`<tds:GetServicesResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl">` +
		`<tds:Service><tds:Namespace>http://www.onvif.org/ver10/device/wsdl</tds:Namespace><tds:XAddr>http://host/onvif/device_service</tds:XAddr></tds:Service>` +
		`</tds:GetServicesResponse></soapenv:Body></soapenv:Envelope>`)

	got := string(injectEventsService(body, "GetServices", "http://pibell-0:8080/onvif/event_service"))

	if !strings.Contains(got, "http://www.onvif.org/ver10/events/wsdl") {
		t.Errorf("expected events namespace to be present, got: %s", got)
	}
	if !strings.Contains(got, "http://pibell-0:8080/onvif/event_service") {
		t.Errorf("expected events XAddr to be present, got: %s", got)
	}
	if !strings.Contains(got, "</tds:Service></tds:GetServicesResponse>") {
		t.Errorf("expected injected service to be the last Service before the closing tag, got: %s", got)
	}
}

func TestInjectEventsService_GetCapabilities(t *testing.T) {
	body := []byte(`<soapenv:Envelope><soapenv:Body>` +
		`<tds:GetCapabilitiesResponse xmlns:tds="x"><tds:Capabilities>` +
		`<tt:Device xmlns:tt="y">...</tt:Device><tt:Media xmlns:tt="y">...</tt:Media>` +
		`</tds:Capabilities></tds:GetCapabilitiesResponse></soapenv:Body></soapenv:Envelope>`)

	got := string(injectEventsService(body, "GetCapabilities", "http://pibell-0:8080/onvif/event_service"))

	if !strings.Contains(got, "<tt:Events") {
		t.Errorf("expected tt:Events to be injected, got: %s", got)
	}
	if !strings.Contains(got, "</tt:Events></tds:Capabilities>") {
		t.Errorf("expected injected Events to be the last child before Capabilities closes, got: %s", got)
	}
}
