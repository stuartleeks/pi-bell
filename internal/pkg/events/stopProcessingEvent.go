package events

import (
	"encoding/json"

	"github.com/gobuffalo/uuid"
)

// StopProcessingEvent is used to indicate that a client processing loop should stop
type StopProcessingEvent struct {
	EventCommon
	ID uuid.UUID `json:"id"`
}

var _ Event = StopProcessingEvent{}

func NewStopProcessingEvent() *StopProcessingEvent {
	return &StopProcessingEvent{
		EventCommon: EventCommon{
			EventType: EventTypeStopProcessing,
		},
		ID: uuid.Must(uuid.NewV4()),
	}
}

// ToJSON converts the event to JSON
func (e StopProcessingEvent) ToJSON() (string, error) {
	jsonValue, err := json.Marshal(e)
	return string(jsonValue), err
}

func (e StopProcessingEvent) GetType() string {
	return e.EventType
}

// ParseStopProcessingEventJSON parses the JSON representation of a StopProcessingEvent
func ParseStopProcessingEventJSON(jsonValue []byte) (*StopProcessingEvent, error) {
	var stopProcessingEvent StopProcessingEvent
	err := json.Unmarshal(jsonValue, &stopProcessingEvent)
	if err != nil {
		return nil, err
	}
	return &stopProcessingEvent, nil
}

func (e StopProcessingEvent) GetProperties() map[string]string {
	return map[string]string{
		"type": e.EventType,
		"id":   e.ID.String(),
	}
}
