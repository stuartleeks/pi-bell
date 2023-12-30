package events

import (
	"encoding/json"
	"time"

	"github.com/gobuffalo/uuid"
)

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
