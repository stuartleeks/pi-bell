package events

import (
	"encoding/json"
)

const (
	EventTypeButton         = "button-event"
	EventTypeSnooze         = "snooze-event"
	EventTypeUnSnooze       = "unsnooze-event"
	EventTypeStopProcessing = "stop-processing-event"
)

type EventCommon struct {
	EventType string `json:"eventType"`
}
type Event interface {
	GetType() string
	GetProperties() map[string]string
	ToJSON() (string, error)
}

// ParseEventJSON parses the JSON representation of a Event
func ParseEventJSON(jsonValue []byte) (*EventCommon, error) {

	var event EventCommon
	err := json.Unmarshal(jsonValue, &event)
	if err != nil {
		return nil, err
	}
	return &event, nil
}
