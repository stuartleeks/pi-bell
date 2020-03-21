package gpio

import (
	"github.com/warthog618/gpiod"
)

// Button represents a button connected to GPIO
type Button struct {
	line *gpiod.Line
}

// NewButton creates a new Button instance
func NewButton(chip *gpiod.Chip, pin int, handler func(buttonPressed bool)) (*Button, error) {
	line, err := chip.RequestLine(pin, gpiod.WithBothEdges(func(evt gpiod.LineEvent) {
		buttonPressed := true
		if evt.Type == gpiod.LineEventFallingEdge {
			buttonPressed = false
		}
		handler(buttonPressed)
	}))
	if err != nil {
		return nil, err
	}

	return &Button{
		line: line,
	}, nil
}

// Close releases button resources
func (b *Button) Close() error {
	return b.line.Close()
}
