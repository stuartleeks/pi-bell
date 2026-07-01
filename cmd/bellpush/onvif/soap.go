package onvif

import (
	"fmt"
	"html"
	"net/http"
)

// soapNamespaces holds the XML namespace declarations used on the outer
// soapenv:Envelope of every response this package emits directly (i.e. not
// proxied through to go2rtc unchanged).
const soapNamespaces = `xmlns:soapenv="http://www.w3.org/2003/05/soap-envelope" ` +
	`xmlns:tev="http://www.onvif.org/ver10/events/wsdl" ` +
	`xmlns:tt="http://www.onvif.org/ver10/schema" ` +
	`xmlns:tns1="http://www.onvif.org/ver10/topics" ` +
	`xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2" ` +
	`xmlns:wsa="http://www.w3.org/2005/08/addressing"`

// envelope wraps a SOAP Body fragment in a standard Envelope with the
// namespaces this package's responses use.
func envelope(body string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>`+
		`<soapenv:Envelope %s><soapenv:Body>%s</soapenv:Body></soapenv:Envelope>`,
		soapNamespaces, body)
}

// writeSOAP writes a SOAP envelope response with the appropriate content type.
func writeSOAP(w http.ResponseWriter, statusCode int, body string) {
	w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
	w.WriteHeader(statusCode)
	_, _ = w.Write([]byte(envelope(body)))
}

// writeSOAPFault writes a minimal SOAP 1.2 fault response.
func writeSOAPFault(w http.ResponseWriter, statusCode int, code, reason string) {
	body := fmt.Sprintf(
		`<soapenv:Fault>`+
			`<soapenv:Code><soapenv:Value>soapenv:%s</soapenv:Value></soapenv:Code>`+
			`<soapenv:Reason><soapenv:Text xml:lang="en">%s</soapenv:Text></soapenv:Reason>`+
			`</soapenv:Fault>`,
		xmlEscape(code), xmlEscape(reason))
	writeSOAP(w, statusCode, body)
}

// xmlEscape escapes text for safe inclusion in XML element/attribute content.
func xmlEscape(s string) string {
	return html.EscapeString(s)
}
