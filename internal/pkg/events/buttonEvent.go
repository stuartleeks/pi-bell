package events

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gobuffalo/uuid"
)

const (
	EventTypeButton = "button-event"
	EventTypeSnooze = "snooze-event"
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

// ButtonEventType indicates the type of button event
type ButtonEventType int

const (
	// ButtonPressed occurs when a button is pressed
	ButtonPressed ButtonEventType = iota
	// ButtonReleased occurs when a button is released after being pressed
	ButtonReleased
)

// ButtonEvent represents an event for a button
type ButtonEvent struct {
	EventCommon
	ID              uuid.UUID       `json:"id"`
	ButtonEventType ButtonEventType `json:"buttonEventType"`
	Source          string          `json:"source"`
}

func NewButtonEvent(buttonEventType ButtonEventType, source string) *ButtonEvent {
	return &ButtonEvent{
		EventCommon: EventCommon{
			EventType: EventTypeButton,
		},
		ID:              uuid.Must(uuid.NewV4()),
		ButtonEventType: buttonEventType,
		Source:          source,
	}
}

var _ Event = ButtonEvent{}

func TypeToString(eventType ButtonEventType) string {
	switch eventType {
	case ButtonPressed:
		return "pressed"
	case ButtonReleased:
		return "released"
	default:
		return fmt.Sprintf("%d", eventType)
	}
}

// ToJSON converts the event to JSON
func (e ButtonEvent) ToJSON() (string, error) {
	jsonValue, err := json.Marshal(e)
	return string(jsonValue), err
}
func (e ButtonEvent) GetType() string {
	return e.EventType
}

// ParseButtonEventJSON parses the JSON representation of a ButtonEvent
func ParseButtonEventJSON(jsonValue []byte) (*ButtonEvent, error) {

	var buttonEvent ButtonEvent
	err := json.Unmarshal(jsonValue, &buttonEvent)
	if err != nil {
		return nil, err
	}
	return &buttonEvent, nil
}

func (e ButtonEvent) GetProperties() map[string]string {
	return map[string]string{
		"type":            e.EventType,
		"id":              e.ID.String(),
		"buttonEventType": TypeToString(e.ButtonEventType),
		"source":          e.Source,
	}
}

// SnoozeEventType indicates the type of snooze event
type SnoozeEvent struct {
	EventCommon
	ID           uuid.UUID `json:"id"`
	SnoozeExpiry time.Time `json:"snoozeExpiry"`
}

var _ Event = SnoozeEvent{}

func NewSnoozeEvent(snoozeExpiry time.Time) *SnoozeEvent {
	return &SnoozeEvent{
		EventCommon: EventCommon{
			EventType: EventTypeSnooze,
		},
		ID:           uuid.Must(uuid.NewV4()),
		SnoozeExpiry: snoozeExpiry,
	}
}

// ToJSON converts the event to JSON
func (e SnoozeEvent) ToJSON() (string, error) {
	jsonValue, err := json.Marshal(e)
	return string(jsonValue), err
}

func (e SnoozeEvent) GetType() string {
	return e.EventType
}

// ParseSnoozeEventJSON parses the JSON representation of a SnoozeEvent
func ParseSnoozeEventJSON(jsonValue []byte) (*SnoozeEvent, error) {
	var snoozeEvent SnoozeEvent
	err := json.Unmarshal(jsonValue, &snoozeEvent)
	if err != nil {
		return nil, err
	}
	return &snoozeEvent, nil
}

func (e SnoozeEvent) GetProperties() map[string]string {
	return map[string]string{
		"type":         e.EventType,
		"id":           e.ID.String(),
		"snoozeExpiry": e.SnoozeExpiry.String(),
	}
}
