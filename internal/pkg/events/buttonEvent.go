package events

import "encoding/json"

//ButtonEventType indicates the type of button event
type ButtonEventType int

const (
	// ButtonPressed occurs when a button is pressed
	ButtonPressed ButtonEventType = iota
	// ButtonReleased occurs when a button is released after being pressed
	ButtonReleased
)

// ButtonEvent represents an event for a button
type ButtonEvent struct {
	Type ButtonEventType `json:"type"`
}

// ToJSON converts the event to JSON
func (e *ButtonEvent) ToJSON() (string, error) {
	jsonValue, err := json.Marshal(e)
	return string(jsonValue), err
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
