package bellpush

import (
	"log"
	"sync"
	"time"
)

// Default motion sensor smoothing parameters. These mirror gpiozero's
// MotionSensor configuration (queue_len=10, sample_rate=10, threshold=0.5):
// the sensor is sampled 10 times a second, the most recent 10 samples (1s) are
// averaged, and motion is considered active when more than half are "high".
//
// Deliberately, there is no separate "hold time"/cooldown layered on top of
// the window. An earlier version of this code added a long (10s) quiet period
// that had to elapse after the window cleared before "stopped" was reported,
// intended to bridge the on/off pulsing of non-retriggering PIR modules.
// Comparing against a reference gpiozero implementation showed that gpiozero
// itself has no such hold time - it fires when_no_motion the instant the
// smoothed value drops back below threshold - and that the extra cooldown was
// actively harmful: while waiting out the (much longer than the PIR's own
// hardware retrigger pulse) hold period, the PIR's own hardware could pulse
// high again, so by the time "stopped" finally fired a fresh hardware pulse
// had often already arrived, immediately re-triggering "detected" right after
// "stopped". It also made "stopped" fire several seconds later than gpiozero
// (~8s vs ~3s), most of which was pure artificial delay rather than sensor
// behavior. If a PIR proves too twitchy, tune the window itself (increase
// QueueLen and/or SampleRate) rather than adding a downstream hold timer.
const (
	defaultMotionQueueLen   = 10
	defaultMotionSampleRate = 100 * time.Millisecond // 10 Hz
	defaultMotionThreshold  = 0.5
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

// run polls the sensor, maintaining a rolling window of the most recent
// samples, and fires onDetected/onStopped directly on each edge transition of
// the windowed average across the threshold. This mirrors gpiozero's
// SmoothedInputDevice/MotionSensor: the mean of the queued samples ("value")
// is compared to "threshold" to give "is_active", and when_motion/
// when_no_motion fire immediately whenever is_active changes - there is no
// additional debounce/cooldown layered on top of the window. All jitter
// filtering comes from the size of the window (QueueLen/SampleRate), not from
// a downstream hold timer; if a PIR proves too twitchy, widen the window
// rather than adding a hold time back in (see the comment above the defaults
// for why an earlier hold-time approach was actively counter-productive).
//
// To avoid a spurious detection while the sensor settles, events are
// suppressed until the window has filled and first reports no motion (the
// equivalent of gpiozero's wait_for_no_motion warm-up / partial=False
// behavior).
func (m *motionSensor) run() {
	samples := make([]int, 0, m.config.QueueLen)
	sum := 0
	active := false
	ready := false

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
				case windowHigh && !active:
					active = true
					m.onDetected()
				case !windowHigh && active:
					active = false
					m.onStopped()
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

// MotionState is a concurrency-safe record of the PIR motion sensor's current
// state (active/inactive) plus the time it last changed. It is shared between
// the motion sensor callbacks (and the "m"/"n" stdio simulation keys) and any
// consumer that needs to observe motion transitions - e.g. an ONVIF Events
// service - without coupling to the webhook notifier.
type MotionState struct {
	mu        sync.RWMutex
	active    bool
	changedAt time.Time
	// onChange, if set, is invoked (outside the lock) whenever SetActive
	// actually changes the active state, with the new state and the time of
	// the change. Consumers (e.g. an ONVIF PullPoint subscription manager)
	// can use this to be notified of transitions without polling.
	onChange func(active bool, changedAt time.Time)
}

// NewMotionState creates a MotionState initialized to inactive.
func NewMotionState() *MotionState {
	return &MotionState{
		changedAt: time.Now(),
	}
}

// SetActive updates the motion state. If the value differs from the current
// state, changedAt is updated to now and any registered onChange callback is
// invoked. Calling with the same value is a no-op (no callback, no timestamp
// change).
func (s *MotionState) SetActive(active bool) {
	s.mu.Lock()
	if s.active == active {
		s.mu.Unlock()
		return
	}
	s.active = active
	s.changedAt = time.Now()
	changedAt := s.changedAt
	onChange := s.onChange
	s.mu.Unlock()

	if onChange != nil {
		onChange(active, changedAt)
	}
}

// Get returns the current active state and the time it last changed.
func (s *MotionState) Get() (active bool, changedAt time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active, s.changedAt
}

// OnChange registers a callback invoked whenever the state actually changes.
// Only one callback is supported; a later call replaces any previous one.
func (s *MotionState) OnChange(f func(active bool, changedAt time.Time)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onChange = f
}
