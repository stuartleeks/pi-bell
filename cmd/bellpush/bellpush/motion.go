package bellpush

import (
	"log"
	"math"
	"time"
)

// Default motion sensor smoothing parameters. These mirror a stable gpiozero
// MotionSensor configuration (queue_len=10, sample_rate=10, threshold=0.5):
// the sensor is sampled 10 times a second, the most recent 10 samples (1s) are
// averaged, and motion is considered active when more than half are "high".
//
// defaultMotionHoldTime adds a cooldown ("linger") before motion is reported as
// stopped. Many PIR modules in non-retriggering mode pulse on/off while a person
// is still present; the hold time bridges those gaps so a continuous presence
// produces a single detected/stopped pair rather than rapid repeats.
const (
	defaultMotionQueueLen   = 10
	defaultMotionSampleRate = 100 * time.Millisecond // 10 Hz
	defaultMotionThreshold  = 0.5
	defaultMotionHoldTime   = 10 * time.Second
)

// digitalReader reads the digital state of a GPIO pin. *raspi.Adaptor satisfies
// this interface; tests can supply a fake.
type digitalReader interface {
	DigitalRead(pin string) (int, error)
}

// MotionConfig holds the smoothing/debounce parameters for the PIR motion
// sensor. Any zero/invalid field falls back to the corresponding default.
type MotionConfig struct {
	// Pin is the GPIO pin the sensor is attached to. Set internally; callers
	// configuring tuning parameters can leave this empty.
	Pin string
	// QueueLen is the number of recent samples averaged together.
	QueueLen int
	// SampleRate is the interval between samples.
	SampleRate time.Duration
	// Threshold is the fraction of high samples (0-1) required for motion.
	Threshold float64
	// HoldTime is how long the sensor must be continuously quiet before motion
	// is reported as stopped.
	HoldTime time.Duration
}

// motionSensor polls a PIR sensor and applies a rolling-average filter plus a
// hold time to avoid the rapid detected/stopped flapping seen when reacting to
// raw pin transitions.
type motionSensor struct {
	reader     digitalReader
	config     MotionConfig
	onDetected func()
	onStopped  func()
	halt       chan struct{}
}

// newMotionSensor creates a motionSensor. Any config value that is zero/invalid
// falls back to the corresponding default.
func newMotionSensor(reader digitalReader, config MotionConfig, onDetected, onStopped func()) *motionSensor {
	if config.QueueLen < 1 {
		config.QueueLen = defaultMotionQueueLen
	}
	if config.SampleRate <= 0 {
		config.SampleRate = defaultMotionSampleRate
	}
	if config.Threshold <= 0 || config.Threshold >= 1 {
		config.Threshold = defaultMotionThreshold
	}
	if config.HoldTime <= 0 {
		config.HoldTime = defaultMotionHoldTime
	}
	return &motionSensor{
		reader:     reader,
		config:     config,
		onDetected: onDetected,
		onStopped:  onStopped,
		halt:       make(chan struct{}),
	}
}

// Start begins polling the sensor in a background goroutine.
func (m *motionSensor) Start() {
	go m.run()
}

// Stop halts polling.
func (m *motionSensor) Stop() {
	close(m.halt)
}

// run polls the sensor, maintaining a rolling window of the most recent samples.
// The window is "high" when the fraction of high samples exceeds the threshold,
// which filters out brief jitter. Motion is reported as detected on the first
// high window, and stopped only after the window has stayed low continuously for
// the configured hold time (expressed as a number of samples). This bridges the
// on/off pulsing of non-retriggering PIR modules so sustained presence yields a
// single detected/stopped pair.
//
// To avoid a spurious detection while the sensor settles, events are suppressed
// until the window has filled and first reported no motion (the equivalent of
// gpiozero's wait_for_no_motion warm-up).
func (m *motionSensor) run() {
	holdSamples := int(math.Ceil(float64(m.config.HoldTime) / float64(m.config.SampleRate)))
	if holdSamples < 1 {
		holdSamples = 1
	}

	samples := make([]int, 0, m.config.QueueLen)
	sum := 0
	active := false
	ready := false
	quietSamples := 0

	for {
		value, err := m.reader.DigitalRead(m.config.Pin)
		if err != nil {
			log.Printf("Error reading motion sensor pin %s: %v\n", m.config.Pin, err)
		} else {
			if value != 0 {
				value = 1
			}
			if len(samples) >= m.config.QueueLen {
				sum -= samples[0]
				samples = samples[1:]
			}
			samples = append(samples, value)
			sum += value

			if len(samples) >= m.config.QueueLen {
				windowHigh := float64(sum)/float64(len(samples)) > m.config.Threshold
				switch {
				case !ready:
					// Warm-up: wait until the sensor settles to no motion
					// before reacting to avoid a spurious initial detection.
					if !windowHigh {
						ready = true
						active = false
					}
				case windowHigh:
					quietSamples = 0
					if !active {
						active = true
						m.onDetected()
					}
				case active:
					// Window is low while motion is active: hold until the
					// sensor has been quiet for the full hold time.
					quietSamples++
					if quietSamples >= holdSamples {
						active = false
						m.onStopped()
					}
				}
			}
		}

		select {
		case <-time.After(m.config.SampleRate):
		case <-m.halt:
			return
		}
	}
}
