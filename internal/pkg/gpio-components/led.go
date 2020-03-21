package gpio

import (
	"github.com/warthog618/gpiod"
)

// Led represents a relay connected to GPIO
type Led struct {
	line *gpiod.Line
}

// NewLed creates a new Relay instance
func NewLed(chip *gpiod.Chip, pin int) (*Led, error) {
	line, err := chip.RequestLine(pin, gpiod.AsOutput(0))
	if err != nil {
		return nil, err
	}

	return &Led{
		line: line,
	}, nil
}

// IsOn returns true if the Led is on
func (l *Led) IsOn() (bool, error) {
	value, err := l.line.Value()
	if err != nil {
		return false, err
	}
	return value == 1, nil
}

// On turns the Led on
func (l *Led) On() error {
	return l.line.SetValue(1)
}

// Off turns the Led off
func (l *Led) Off() error {
	return l.line.SetValue(0)
}

// Toggle sets the Led to On if currently Off and vice versa
func (l *Led) Toggle() error {
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
func (r *Led) Close() error {
	return r.line.Close()
}
