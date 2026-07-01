package onvif

import (
	"testing"
	"time"

	"github.com/stuartleeks/pi-bell/cmd/bellpush/bellpush"
)

func TestManager_CreateAndPullInitializedMessage(t *testing.T) {
	state := bellpush.NewMotionState()
	m := NewManager(state, "cell")

	id, expiry := m.Create(0, false)
	if id == "" {
		t.Fatalf("expected non-empty subscription id")
	}
	if !expiry.After(time.Now()) {
		t.Fatalf("expected expiry in the future")
	}

	messages, ok := m.Pull(id, 100*time.Millisecond, 0)
	if !ok {
		t.Fatalf("expected pull to succeed for known subscription")
	}
	if len(messages) != 1 || messages[0].Operation != "Initialized" || messages[0].Active {
		t.Fatalf("expected a single Initialized(false) message, got %v", messages)
	}
}

func TestManager_BroadcastsChangeToSubscribers(t *testing.T) {
	state := bellpush.NewMotionState()
	m := NewManager(state, "cell")

	id, _ := m.Create(0, false)
	// Drain the initial "Initialized" message.
	if _, ok := m.Pull(id, 10*time.Millisecond, 0); !ok {
		t.Fatalf("expected initial pull to succeed")
	}

	state.SetActive(true)

	messages, ok := m.Pull(id, 500*time.Millisecond, 0)
	if !ok {
		t.Fatalf("expected pull to succeed")
	}
	if len(messages) != 1 || messages[0].Operation != "Changed" || !messages[0].Active {
		t.Fatalf("expected a single Changed(true) message, got %v", messages)
	}
}

func TestManager_PullUnknownSubscription(t *testing.T) {
	state := bellpush.NewMotionState()
	m := NewManager(state, "cell")

	if _, ok := m.Pull("does-not-exist", 10*time.Millisecond, 0); ok {
		t.Fatalf("expected pull for unknown subscription to fail")
	}
}

func TestManager_PullTimesOutWithEmptyResult(t *testing.T) {
	state := bellpush.NewMotionState()
	m := NewManager(state, "cell")

	id, _ := m.Create(0, false)
	if _, ok := m.Pull(id, 10*time.Millisecond, 0); !ok {
		t.Fatalf("expected initial pull to succeed")
	}

	start := time.Now()
	messages, ok := m.Pull(id, 50*time.Millisecond, 0)
	elapsed := time.Since(start)
	if !ok {
		t.Fatalf("expected timed-out pull to still be ok (valid empty result)")
	}
	if len(messages) != 0 {
		t.Fatalf("expected no messages, got %v", messages)
	}
	if elapsed < 40*time.Millisecond {
		t.Fatalf("expected pull to block roughly for the timeout, elapsed %v", elapsed)
	}
}

func TestManager_RenewAndUnsubscribe(t *testing.T) {
	state := bellpush.NewMotionState()
	m := NewManager(state, "cell")

	id, _ := m.Create(0, false)

	newExpiry, ok := m.Renew(id, 0)
	if !ok || !newExpiry.After(time.Now()) {
		t.Fatalf("expected renew to succeed with future expiry")
	}

	if !m.Unsubscribe(id) {
		t.Fatalf("expected unsubscribe to succeed")
	}
	if _, ok := m.Pull(id, 10*time.Millisecond, 0); ok {
		t.Fatalf("expected pull after unsubscribe to fail")
	}
	if m.Unsubscribe(id) {
		t.Fatalf("expected second unsubscribe of same id to fail")
	}
}

func TestManager_MultipleSubscriptionsEachGetTransition(t *testing.T) {
	state := bellpush.NewMotionState()
	m := NewManager(state, "cell")

	id1, _ := m.Create(0, false)
	id2, _ := m.Create(0, false)
	// Drain initial messages.
	m.Pull(id1, 10*time.Millisecond, 0)
	m.Pull(id2, 10*time.Millisecond, 0)

	state.SetActive(true)

	msgs1, ok1 := m.Pull(id1, 200*time.Millisecond, 0)
	msgs2, ok2 := m.Pull(id2, 200*time.Millisecond, 0)
	if !ok1 || !ok2 || len(msgs1) != 1 || len(msgs2) != 1 {
		t.Fatalf("expected both subscriptions to receive the transition, got %v / %v", msgs1, msgs2)
	}
}
