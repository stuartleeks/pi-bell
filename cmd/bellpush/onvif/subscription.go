package onvif

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/stuartleeks/pi-bell/cmd/bellpush/bellpush"
)

// defaultSubscriptionDuration is used when a client does not request a specific
// InitialTerminationTime/Timeout on CreatePullPointSubscription/Renew.
const defaultSubscriptionDuration = 5 * time.Minute

// minSubscriptionDuration / maxSubscriptionDuration bound whatever a client requests.
const (
	minSubscriptionDuration = 10 * time.Second
	maxSubscriptionDuration = 30 * time.Minute
)

// Notification is a single ONVIF motion event, queued for delivery to a
// subscription's next PullMessages call.
type Notification struct {
	// Operation is the ONVIF PropertyOperation: "Initialized" for the first
	// message on a subscription, "Changed" for subsequent PIR transitions.
	Operation string
	Active    bool
	UTCTime   time.Time
}

// subscription is a single ONVIF PullPoint subscription, addressed by ID.
type subscription struct {
	id     string
	mu     sync.Mutex
	queue  []Notification
	signal chan struct{}
	expiry time.Time
}

func (s *subscription) enqueue(n Notification) {
	s.mu.Lock()
	s.queue = append(s.queue, n)
	s.mu.Unlock()
	select {
	case s.signal <- struct{}{}:
	default:
	}
}

// Manager tracks active ONVIF PullPoint subscriptions and feeds them
// notifications driven by the shared bellpush.MotionState.
type Manager struct {
	mu    sync.Mutex
	subs  map[string]*subscription
	topic topicInfo
}

// NewManager creates a Manager for the given motion topic mode ("cell" or
// "alarm", see topicForMode) and registers a callback on motionState so every
// active subscription is notified of PIR transitions.
func NewManager(motionState *bellpush.MotionState, topicMode string) *Manager {
	m := &Manager{
		subs:  make(map[string]*subscription),
		topic: topicForMode(topicMode),
	}
	motionState.OnChange(func(active bool, changedAt time.Time) {
		m.broadcastChange(active, changedAt)
	})
	return m
}

func (m *Manager) broadcastChange(active bool, changedAt time.Time) {
	m.mu.Lock()
	subs := make([]*subscription, 0, len(m.subs))
	for _, s := range m.subs {
		subs = append(subs, s)
	}
	m.mu.Unlock()

	for _, s := range subs {
		s.enqueue(Notification{Operation: "Changed", Active: active, UTCTime: changedAt})
	}
}

// clampDuration bounds a requested subscription duration to sane limits,
// falling back to defaultSubscriptionDuration when requested is zero/negative.
func clampDuration(requested time.Duration) time.Duration {
	if requested <= 0 {
		return defaultSubscriptionDuration
	}
	if requested < minSubscriptionDuration {
		return minSubscriptionDuration
	}
	if requested > maxSubscriptionDuration {
		return maxSubscriptionDuration
	}
	return requested
}

// Create starts a new PullPoint subscription, seeding it with an "Initialized"
// Notification reflecting the current motion state, and returns its ID and
// expiry time.
func (m *Manager) Create(requestedDuration time.Duration, currentActive bool) (id string, expiry time.Time) {
	duration := clampDuration(requestedDuration)
	now := time.Now()
	sub := &subscription{
		id:     newSubscriptionID(),
		signal: make(chan struct{}, 1),
		expiry: now.Add(duration),
	}
	sub.queue = append(sub.queue, Notification{Operation: "Initialized", Active: currentActive, UTCTime: now})

	m.mu.Lock()
	m.reapLocked()
	m.subs[sub.id] = sub
	m.mu.Unlock()

	return sub.id, sub.expiry
}

// Pull waits up to timeout for at least one Notification to be available on
// the given subscription, then returns up to limit of them (0 or negative
// means "no limit"). ok is false if the subscription does not exist or has
// expired. A timeout with no notifications available returns an empty, but
// valid (ok=true), result - this is normal PullPoint behavior.
func (m *Manager) Pull(id string, timeout time.Duration, limit int) (messages []Notification, ok bool) {
	sub := m.get(id)
	if sub == nil {
		return nil, false
	}

	deadline := time.Now().Add(timeout)
	for {
		sub.mu.Lock()
		if len(sub.queue) > 0 {
			n := limit
			if n <= 0 || n > len(sub.queue) {
				n = len(sub.queue)
			}
			msgs := sub.queue[:n]
			sub.queue = sub.queue[n:]
			sub.mu.Unlock()
			return msgs, true
		}
		sub.mu.Unlock()

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, true
		}
		select {
		case <-sub.signal:
			continue
		case <-time.After(remaining):
			return nil, true
		}
	}
}

// Renew extends a subscription's expiry and returns the new value. ok is false
// if the subscription does not exist or has already expired.
func (m *Manager) Renew(id string, requestedDuration time.Duration) (expiry time.Time, ok bool) {
	sub := m.get(id)
	if sub == nil {
		return time.Time{}, false
	}
	duration := clampDuration(requestedDuration)
	newExpiry := time.Now().Add(duration)

	m.mu.Lock()
	sub.expiry = newExpiry
	m.mu.Unlock()

	return newExpiry, true
}

// Unsubscribe removes a subscription. ok is false if it did not exist.
func (m *Manager) Unsubscribe(id string) (ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.subs[id]; !exists {
		return false
	}
	delete(m.subs, id)
	return true
}

// get returns the subscription for id, or nil if it does not exist or has expired.
func (m *Manager) get(id string) *subscription {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reapLocked()
	return m.subs[id]
}

// reapLocked removes expired subscriptions. Callers must hold m.mu.
func (m *Manager) reapLocked() {
	now := time.Now()
	for id, sub := range m.subs {
		if now.After(sub.expiry) {
			delete(m.subs, id)
		}
	}
}

// newSubscriptionID generates a random hex subscription ID.
func newSubscriptionID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand read failures are effectively unheard-of in practice; fall
		// back to a timestamp-derived ID rather than panicking.
		return "sub-" + time.Now().Format("20060102150405.000000000")
	}
	return "sub-" + hex.EncodeToString(b)
}
