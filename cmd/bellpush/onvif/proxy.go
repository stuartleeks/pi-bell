package onvif

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
)

// reXAddr rewrites the host:port of every <XAddr> element (any namespace prefix)
// in an ONVIF SOAP response, preserving the scheme and path. This is used to
// point Device/Media (and our injected Events) XAddr entries back at bellpush
// itself, rather than at the go2rtc sidecar, so that Protect routes all further
// ONVIF calls through bellpush (see motion-onvif.md "Architecture").
var reXAddr = regexp.MustCompile(`(?is)(<(?:[\w-]+:)?XAddr>)(https?://)([^/<\s]+)([^<]*)(</(?:[\w-]+:)?XAddr>)`)

// rewriteXAddrHost replaces the host:port portion of every XAddr in body with host.
func rewriteXAddrHost(body []byte, host string) []byte {
	return reXAddr.ReplaceAll(body, []byte(`${1}${2}`+host+`${4}${5}`))
}

// insertBeforeLastClosingTag inserts injected immediately before the last
// occurrence of the closing tag for localName (matching any namespace prefix,
// e.g. </tds:GetServicesResponse> or </GetServicesResponse>). Returns the
// original body unchanged (with ok=false) if no matching closing tag is found.
func insertBeforeLastClosingTag(body []byte, localName string, injected string) ([]byte, bool) {
	re := regexp.MustCompile(fmt.Sprintf(`(?is)</(?:[\w-]+:)?%s>`, localName))
	locs := re.FindAllIndex(body, -1)
	if len(locs) == 0 {
		return body, false
	}
	last := locs[len(locs)-1]
	var out bytes.Buffer
	out.Write(body[:last[0]])
	out.WriteString(injected)
	out.Write(body[last[0]:])
	return out.Bytes(), true
}

// injectEventsService adds an Events service/capability entry to a proxied
// GetServices or GetCapabilities response so Protect discovers bellpush's own
// ONVIF Events (PullPoint) implementation. Namespace prefixes used by the
// injected fragment are declared locally so the result is well-formed
// regardless of the prefixes go2rtc's own response happens to use.
func injectEventsService(body []byte, operation string, eventsXAddr string) []byte {
	switch operation {
	case "GetServices":
		fragment := fmt.Sprintf(
			`<tds:Service xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:tev="http://www.onvif.org/ver10/events/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">`+
				`<tds:Namespace>http://www.onvif.org/ver10/events/wsdl</tds:Namespace>`+
				`<tds:XAddr>%s</tds:XAddr>`+
				`<tds:Capabilities><tev:Capabilities WSSubscriptionPolicySupport="false" WSPullPointSupport="true" WSPausableSubscriptionManagerInterfaceSupport="false"/></tds:Capabilities>`+
				`<tds:Version><tt:Major>2</tt:Major><tt:Minor>5</tt:Minor></tds:Version>`+
				`</tds:Service>`,
			xmlEscape(eventsXAddr))
		if out, ok := insertBeforeLastClosingTag(body, "GetServicesResponse", fragment); ok {
			return out
		}
		log.Printf("onvif: could not find GetServicesResponse closing tag to inject Events service; leaving response unchanged\n")
	case "GetCapabilities":
		fragment := fmt.Sprintf(
			`<tt:Events xmlns:tt="http://www.onvif.org/ver10/schema">`+
				`<tt:XAddr>%s</tt:XAddr>`+
				`<tt:WSSubscriptionPolicySupport>false</tt:WSSubscriptionPolicySupport>`+
				`<tt:WSPullPointSupport>true</tt:WSPullPointSupport>`+
				`<tt:WSPausableSubscriptionManagerInterfaceSupport>false</tt:WSPausableSubscriptionManagerInterfaceSupport>`+
				`</tt:Events>`,
			xmlEscape(eventsXAddr))
		if out, ok := insertBeforeLastClosingTag(body, "Capabilities", fragment); ok {
			return out
		}
		log.Printf("onvif: could not find Capabilities closing tag to inject Events capability; leaving response unchanged\n")
	}
	return body
}

// proxyDeviceService forwards a Device/Media SOAP request unchanged to the
// go2rtc sidecar, at the same path as the incoming request (go2rtc exposes
// Device operations at /onvif/device_service and Media operations at a
// separate /onvif/media_service - see GetCapabilities' Device/Media XAddr
// entries), then (for GetServices/GetCapabilities) rewrites XAddr hosts to
// point back at bellpush and injects an Events service/capability entry so
// Protect discovers this package's Events implementation.
func (h *Handler) proxyDeviceService(w http.ResponseWriter, r *http.Request, body []byte, operation string) {
	upstreamURL := h.go2rtcURL + r.URL.Path
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("onvif: error building upstream request: %v\n", err)
		writeSOAPFault(w, http.StatusBadGateway, "Receiver", "error proxying to go2rtc")
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	} else {
		req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		log.Printf("onvif: error proxying to go2rtc: %v\n", err)
		writeSOAPFault(w, http.StatusBadGateway, "Receiver", "error proxying to go2rtc")
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("onvif: error reading go2rtc response: %v\n", err)
		writeSOAPFault(w, http.StatusBadGateway, "Receiver", "error reading go2rtc response")
		return
	}

	if resp.StatusCode == http.StatusOK && (operation == "GetServices" || operation == "GetCapabilities") {
		respBody = rewriteXAddrHost(respBody, r.Host)
		respBody = injectEventsService(respBody, operation, h.eventServiceURL(r))
	} else if resp.StatusCode == http.StatusOK {
		respBody = rewriteXAddrHost(respBody, r.Host)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)
}

// eventServiceURL returns the absolute URL bellpush itself serves the Events
// (PullPoint) service at, derived from the incoming request's Host and scheme.
func (h *Handler) eventServiceURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/onvif/event_service", scheme, r.Host)
}
