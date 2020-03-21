package gpio

import (
	"github.com/warthog618/gpiod"
)

// Relay represents a relay connected to GPIO
type Relay struct {
	line *gpiod.Line
}

// NewRelay creates a new Relay instance
func NewRelay(chip *gpiod.Chip, pin int) (*Relay, error) {
	line, err := chip.RequestLine(pin, gpiod.AsOutput(1))
	if err != nil {
		return nil, err
	}

	return &Relay{
		line: line,
	}, nil
}

// IsOn returns true if the Relay is on
func (l *Relay) IsOn() (bool, error) {
	value, err := l.line.Value()
	if err != nil {
		return false, err
	}
	return value == 0, nil
}

// On turns the Relay on
func (l *Relay) On() error {
	return l.line.SetValue(0)
}

// Off turns the Relay off
func (l *Relay) Off() error {
	return l.line.SetValue(1)
}

// Toggle sets the Relay to On if currently Off and vice versa
func (l *Relay) Toggle() error {
	on, err := l.IsOn()
	if err != nil {
		return err
	}
	if on {
		err = l.Off()
	} else {
		err = l.On()
	}
	return err
}

// Close releases button resources
func (r *Relay) Close() error {
	return r.line.Close()
}
