package events

import (
	"encoding/json"
	"fmt"

	"github.com/gobuffalo/uuid"
)

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
