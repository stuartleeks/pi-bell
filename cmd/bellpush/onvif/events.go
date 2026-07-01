package onvif

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

func formatUTC(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// EventService handles all requests to the Events (PullPoint) endpoint:
// GetServiceCapabilities, GetEventProperties, CreatePullPointSubscription,
// PullMessages, Renew and Unsubscribe. Unlike Device/Media operations, none of
// these are proxied to go2rtc - they are implemented entirely in this package,
// fed by the shared bellpush.MotionState (see motion-onvif.md Phase 3).
func (h *Handler) EventService(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}

	switch detectOperation(body) {
	case "GetServiceCapabilities":
		h.handleGetServiceCapabilities(w)
	case "GetEventProperties":
		h.handleGetEventProperties(w)
	case "CreatePullPointSubscription":
		h.handleCreatePullPointSubscription(w, r, body)
	case "PullMessages":
		h.handlePullMessages(w, r, body)
	case "Renew":
		h.handleRenew(w, r, body)
	case "Unsubscribe":
		h.handleUnsubscribe(w, r)
	default:
		log.Printf("onvif: event_service received unsupported/unrecognized operation\n")
		writeSOAPFault(w, http.StatusBadRequest, "Sender", "unsupported or unrecognized operation")
	}
}

func (h *Handler) handleGetServiceCapabilities(w http.ResponseWriter) {
	body := `<tev:GetServiceCapabilitiesResponse>` +
		`<tev:Capabilities WSSubscriptionPolicySupport="false" WSPullPointSupport="true" ` +
		`WSPausableSubscriptionManagerInterfaceSupport="false" MaxNotificationProducers="1" ` +
		`MaxPullPoints="10" PersistentNotificationStorage="false"/>` +
		`</tev:GetServiceCapabilitiesResponse>`
	writeSOAP(w, http.StatusOK, body)
}

func (h *Handler) handleGetEventProperties(w http.ResponseWriter) {
	body := `<tev:GetEventPropertiesResponse>` +
		`<tev:TopicNamespaceLocation>http://www.onvif.org/onvif/ver10/topics/topicns.xml</tev:TopicNamespaceLocation>` +
		`<wsnt:FixedTopicSet>true</wsnt:FixedTopicSet>` +
		`<wsnt:TopicSet xmlns:wstop="http://docs.oasis-open.org/wsn/t-1">` + h.manager.topic.TopicSetFragment + `</wsnt:TopicSet>` +
		`<wsnt:TopicExpressionDialect>http://docs.oasis-open.org/wsn/t-1/TopicExpressionDialect/Concrete</wsnt:TopicExpressionDialect>` +
		`<wsnt:TopicExpressionDialect>http://www.onvif.org/ver10/tev/topicExpression/ConcreteSet</wsnt:TopicExpressionDialect>` +
		`<wsnt:MessageContentFilterDialect>http://www.onvif.org/ver10/tev/messageContentFilter/ItemFilter</wsnt:MessageContentFilterDialect>` +
		`</tev:GetEventPropertiesResponse>`
	writeSOAP(w, http.StatusOK, body)
}

func (h *Handler) handleCreatePullPointSubscription(w http.ResponseWriter, r *http.Request, body []byte) {
	requestedDuration := time.Duration(0)
	if text, ok := extractElementText(body, "InitialTerminationTime"); ok {
		if d, err := parseISODuration(text); err == nil {
			requestedDuration = d
		}
	}

	active, _ := h.motionState.Get()
	id, expiry := h.manager.Create(requestedDuration, active)
	now := time.Now()

	respBody := fmt.Sprintf(
		`<tev:CreatePullPointSubscriptionResponse>`+
			`<tev:SubscriptionReference><wsa:Address>%s?sub=%s</wsa:Address></tev:SubscriptionReference>`+
			`<wsnt:CurrentTime>%s</wsnt:CurrentTime>`+
			`<wsnt:TerminationTime>%s</wsnt:TerminationTime>`+
			`</tev:CreatePullPointSubscriptionResponse>`,
		xmlEscape(h.eventServiceURL(r)), xmlEscape(id), formatUTC(now), formatUTC(expiry))
	writeSOAP(w, http.StatusOK, respBody)
}

func (h *Handler) handlePullMessages(w http.ResponseWriter, r *http.Request, body []byte) {
	id, ok := h.subscriptionID(r)
	if !ok {
		writeSOAPFault(w, http.StatusBadRequest, "Sender", "unknown or missing subscription")
		return
	}

	timeout := 30 * time.Second
	if text, ok := extractElementText(body, "Timeout"); ok {
		if d, err := parseISODuration(text); err == nil && d > 0 {
			timeout = d
		}
	}
	const maxPullTimeout = 2 * time.Minute
	if timeout > maxPullTimeout {
		timeout = maxPullTimeout
	}
	limit := extractIntElement(body, "MessageLimit", 0)

	messages, ok := h.manager.Pull(id, timeout, limit)
	if !ok {
		writeSOAPFault(w, http.StatusBadRequest, "Sender", "unknown or expired subscription")
		return
	}

	now := time.Now()
	var notificationsXML string
	for _, n := range messages {
		notificationsXML += h.renderNotification(r, n)
	}

	respBody := fmt.Sprintf(
		`<tev:PullMessagesResponse>`+
			`<tev:CurrentTime>%s</tev:CurrentTime>`+
			`<tev:TerminationTime>%s</tev:TerminationTime>`+
			`%s`+
			`</tev:PullMessagesResponse>`,
		formatUTC(now), formatUTC(now.Add(defaultSubscriptionDuration)), notificationsXML)
	writeSOAP(w, http.StatusOK, respBody)
}

// renderNotification renders a single wsnt:NotificationMessage for n.
func (h *Handler) renderNotification(r *http.Request, n Notification) string {
	boolValue := "false"
	if n.Active {
		boolValue = "true"
	}
	return fmt.Sprintf(
		`<wsnt:NotificationMessage>`+
			`<wsnt:Topic Dialect="http://docs.oasis-open.org/wsn/t-1/TopicExpressionDialect/Concrete">%s</wsnt:Topic>`+
			`<wsnt:ProducerReference><wsa:Address>%s</wsa:Address></wsnt:ProducerReference>`+
			`<wsnt:Message>`+
			`<tt:Message UtcTime="%s" PropertyOperation="%s">`+
			`<tt:Source><tt:SimpleItem Name="Source" Value="%s"/></tt:Source>`+
			`<tt:Data><tt:SimpleItem Name="%s" Value="%s"/></tt:Data>`+
			`</tt:Message>`+
			`</wsnt:Message>`+
			`</wsnt:NotificationMessage>`,
		xmlEscape(h.manager.topic.Topic), xmlEscape(h.eventServiceURL(r)), formatUTC(n.UTCTime), xmlEscape(n.Operation),
		xmlEscape(h.sourceToken), xmlEscape(h.manager.topic.SimpleItemName), boolValue)
}

func (h *Handler) handleRenew(w http.ResponseWriter, r *http.Request, body []byte) {
	id, ok := h.subscriptionID(r)
	if !ok {
		writeSOAPFault(w, http.StatusBadRequest, "Sender", "unknown or missing subscription")
		return
	}

	requestedDuration := time.Duration(0)
	if text, ok := extractElementText(body, "TerminationTime"); ok {
		if d, err := parseISODuration(text); err == nil {
			requestedDuration = d
		}
	}

	expiry, ok := h.manager.Renew(id, requestedDuration)
	if !ok {
		writeSOAPFault(w, http.StatusBadRequest, "Sender", "unknown or expired subscription")
		return
	}

	respBody := fmt.Sprintf(
		`<wsnt:RenewResponse>`+
			`<wsnt:TerminationTime>%s</wsnt:TerminationTime>`+
			`<wsnt:CurrentTime>%s</wsnt:CurrentTime>`+
			`</wsnt:RenewResponse>`,
		formatUTC(expiry), formatUTC(time.Now()))
	writeSOAP(w, http.StatusOK, respBody)
}

func (h *Handler) handleUnsubscribe(w http.ResponseWriter, r *http.Request) {
	id, ok := h.subscriptionID(r)
	if ok {
		h.manager.Unsubscribe(id)
	}
	writeSOAP(w, http.StatusOK, `<wsnt:UnsubscribeResponse/>`)
}

// subscriptionID resolves the subscription a PullMessages/Renew/Unsubscribe
// request addresses, from the "sub" query parameter of the URL we handed out
// in CreatePullPointSubscription's SubscriptionReference.
func (h *Handler) subscriptionID(r *http.Request) (string, bool) {
	id := r.URL.Query().Get("sub")
	if id == "" {
		return "", false
	}
	return id, true
}
