package onvif

import "regexp"

// reBody matches everything after the opening <Body> (or <soapenv:Body>, <SOAP-ENV:Body>, etc.)
// tag, regardless of namespace prefix.
var reBody = regexp.MustCompile(`(?is)<(?:[\w-]+:)?Body[^>]*>(.*)`)

// reOperation matches the first XML element name, again ignoring any namespace prefix.
var reOperation = regexp.MustCompile(`(?s)<(?:[\w-]+:)?([A-Za-z][\w-]*)[\s/>]`)

// detectOperation returns the ONVIF operation name (e.g. "GetServices",
// "CreatePullPointSubscription") carried by a SOAP request body, by finding the
// first child element of the SOAP Body. It ignores namespace prefixes, mirroring
// go2rtc's own lightweight regex-based approach (go2rtc does not use a full SOAP/XML
// unmarshaller for request routing either). Returns "" if no operation could be
// identified.
func detectOperation(body []byte) string {
	bodyMatch := reBody.FindSubmatch(body)
	if bodyMatch == nil {
		return ""
	}
	opMatch := reOperation.FindSubmatch(bodyMatch[1])
	if opMatch == nil {
		return ""
	}
	return string(opMatch[1])
}
