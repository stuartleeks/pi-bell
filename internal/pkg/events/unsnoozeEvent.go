package events

import (
	"encoding/json"

	"github.com/gobuffalo/uuid"
)

// UnSnoozeEventType indicates the type of snooze event
type UnSnoozeEvent struct {
	EventCommon
	ID uuid.UUID `json:"id"`
}

var _ Event = UnSnoozeEvent{}

func NewUnSnoozeEvent() *UnSnoozeEvent {
	return &UnSnoozeEvent{
		EventCommon: EventCommon{
			EventType: EventTypeUnSnooze,
		},
		ID: uuid.Must(uuid.NewV4()),
	}
}

// ToJSON converts the event to JSON
func (e UnSnoozeEvent) ToJSON() (string, error) {
	jsonValue, err := json.Marshal(e)
	return string(jsonValue), err
}

func (e UnSnoozeEvent) GetType() string {
	return e.EventType
}

// ParseUnSnoozeEventJSON parses the JSON representation of a SnoozeEvent
func ParseUnSnoozeEventJSON(jsonValue []byte) (*UnSnoozeEvent, error) {
	var unsnoozeEvent UnSnoozeEvent
	err := json.Unmarshal(jsonValue, &unsnoozeEvent)
	if err != nil {
		return nil, err
	}
	return &unsnoozeEvent, nil
}

func (e UnSnoozeEvent) GetProperties() map[string]string {
	return map[string]string{
		"type": e.EventType,
		"id":   e.ID.String(),
	}
}
