package bellpush

import (
	"sync"
	"testing"
	"time"
)

func TestMotionState_InitialStateInactive(t *testing.T) {
	s := NewMotionState()
	active, changedAt := s.Get()
	if active {
		t.Fatalf("expected initial state to be inactive")
	}
	if changedAt.IsZero() {
		t.Fatalf("expected changedAt to be set on creation")
	}
}

func TestMotionState_SetActiveChangesState(t *testing.T) {
	s := NewMotionState()
	before := time.Now()

	s.SetActive(true)

	active, changedAt := s.Get()
	if !active {
		t.Fatalf("expected state to be active")
	}
	if changedAt.Before(before) {
		t.Fatalf("expected changedAt to be updated to at/after the SetActive call")
	}
}

func TestMotionState_SetActiveSameValueIsNoOp(t *testing.T) {
	s := NewMotionState()
	s.SetActive(true)
	_, firstChangedAt := s.Get()

	time.Sleep(2 * time.Millisecond)
	s.SetActive(true) // no-op, same value

	_, secondChangedAt := s.Get()
	if !firstChangedAt.Equal(secondChangedAt) {
		t.Fatalf("expected changedAt to be unchanged for a repeated SetActive(true) call")
	}
}

func TestMotionState_OnChangeCallbackFiresOnTransition(t *testing.T) {
	s := NewMotionState()

	var mu sync.Mutex
	var calls []bool
	s.OnChange(func(active bool, _ time.Time) {
		mu.Lock()
		calls = append(calls, active)
		mu.Unlock()
	})

	s.SetActive(true)
	s.SetActive(true) // no-op, should not fire again
	s.SetActive(false)

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 || calls[0] != true || calls[1] != false {
		t.Fatalf("expected onChange to fire exactly for the two transitions, got %v", calls)
	}
}
