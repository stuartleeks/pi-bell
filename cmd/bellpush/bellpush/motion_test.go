package bellpush

import (
	"sync"
	"testing"
	"time"
)

// fakeReader returns a scripted sequence of pin values, then repeats the last
// value indefinitely. It is safe for concurrent use.
type fakeReader struct {
	mu     sync.Mutex
	values []int
	idx    int
}

func (f *fakeReader) DigitalRead(_ string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.idx >= len(f.values) {
		if len(f.values) == 0 {
			return 0, nil
		}
		return f.values[len(f.values)-1], nil
	}
	v := f.values[f.idx]
	f.idx++
	return v, nil
}

// drive runs the sensor's sampling loop synchronously for the scripted samples
// plus a few trailing reads, collecting detected/stopped events in order.
func drive(values []int, queueLen int, threshold float64) []string {
	reader := &fakeReader{values: values}
	var mu sync.Mutex
	var events []string
	sampleRate := time.Millisecond
	s := newMotionSensor(
		reader,
		MotionConfig{
			Pin:        "test",
			QueueLen:   queueLen,
			SampleRate: sampleRate,
			Threshold:  threshold,
		},
		func() { mu.Lock(); events = append(events, "detected"); mu.Unlock() },
		func() { mu.Lock(); events = append(events, "stopped"); mu.Unlock() },
	)
	s.Start()
	// Allow enough ticks for every scripted sample (plus the repeated tail) to
	// be processed.
	time.Sleep(time.Duration(len(values)+queueLen+5) * 2 * time.Millisecond)
	s.Stop()
	mu.Lock()
	defer mu.Unlock()
	out := make([]string, len(events))
	copy(out, events)
	return out
}

func TestMotionSensor_WarmupSuppressesInitialEvents(t *testing.T) {
	// Sensor is high immediately on startup; warm-up should suppress any event
	// until it first settles to no motion.
	values := make([]int, 0, 30)
	for i := 0; i < 30; i++ {
		values = append(values, 1)
	}
	events := drive(values, 10, 0.5)
	if len(events) != 0 {
		t.Errorf("expected no events during warm-up while pin stays high, got %v", events)
	}
}

func TestMotionSensor_DetectsAndStopsOnce(t *testing.T) {
	values := []int{}
	// Warm-up: settle low.
	for i := 0; i < 12; i++ {
		values = append(values, 0)
	}
	// Motion: sustained high.
	for i := 0; i < 12; i++ {
		values = append(values, 1)
	}
	// Stop: sustained low (long enough for the window to clear once it fills
	// past queueLen).
	for i := 0; i < 10; i++ {
		values = append(values, 0)
	}

	events := drive(values, 10, 0.5)

	if len(events) != 2 || events[0] != "detected" || events[1] != "stopped" {
		t.Fatalf("expected [detected stopped], got %v", events)
	}
}

func TestMotionSensor_FiltersBriefSpikes(t *testing.T) {
	values := []int{}
	// Warm-up: settle low.
	for i := 0; i < 12; i++ {
		values = append(values, 0)
	}
	// A few isolated spikes interspersed with lows. With queueLen=10 and
	// threshold=0.5, a couple of high samples in a 10-sample window never
	// exceed the threshold, so no detection should fire.
	spikey := []int{1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0}
	values = append(values, spikey...)

	events := drive(values, 10, 0.5)

	if len(events) != 0 {
		t.Errorf("expected brief spikes to be filtered out, got %v", events)
	}
}

func TestMotionSensor_StopsImmediatelyOnceWindowClears(t *testing.T) {
	// Unlike an earlier version of this sensor, there is no separate hold
	// time/cooldown: "stopped" fires as soon as the windowed average drops
	// back below threshold, mirroring gpiozero's MotionSensor (which fires
	// when_no_motion the instant is_active flips, with no extra debounce
	// layered on the smoothing window).
	values := []int{}
	for i := 0; i < 12; i++ { // warm-up low
		values = append(values, 0)
	}
	for i := 0; i < 12; i++ { // motion: sustained high
		values = append(values, 1)
	}
	for i := 0; i < 2; i++ { // brief dip: too small a fraction of the 10-sample
		// window to bring the average back to/below threshold, so the window
		// alone filters it out without needing a hold time.
		values = append(values, 0)
	}
	for i := 0; i < 12; i++ { // motion continues: sustained high again
		values = append(values, 1)
	}
	for i := 0; i < 10; i++ { // long quiet: window clears
		values = append(values, 0)
	}

	events := drive(values, 10, 0.5)

	if len(events) != 2 || events[0] != "detected" || events[1] != "stopped" {
		t.Fatalf("expected [detected stopped], got %v", events)
	}
}
