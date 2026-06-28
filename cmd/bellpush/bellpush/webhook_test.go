package bellpush

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNotify_SendsCorrectRequest(t *testing.T) {
	var gotMethod, gotContentType string
	var gotBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewWebhookNotifier(server.URL, nil)
	notifier.Notify()

	if gotMethod != http.MethodPost {
		t.Errorf("expected method POST, got %s", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Errorf("expected content-type application/json, got %s", gotContentType)
	}

	var payload map[string]string
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("failed to unmarshal body: %v", err)
	}
	if payload["type"] != "doorbell" {
		t.Errorf("expected type doorbell, got %s", payload["type"])
	}
	if payload["title"] != "Doorbell" {
		t.Errorf("expected title Doorbell, got %s", payload["title"])
	}
	if payload["body"] != "Someone is at the door" {
		t.Errorf("expected body 'Someone is at the door', got %s", payload["body"])
	}
	if payload["url"] != "/doorbell/camera" {
		t.Errorf("expected url /doorbell/camera, got %s", payload["url"])
	}
}

func TestNotify_NilNotifierIsNoOp(t *testing.T) {
	var w *WebhookNotifier
	// Should not panic
	w.Notify()
}

func TestNewWebhookNotifier_EmptyURLReturnsNil(t *testing.T) {
	notifier := NewWebhookNotifier("", nil)
	if notifier != nil {
		t.Error("expected nil notifier for empty URL")
	}
}

func TestNotify_NonSuccessStatusDoesNotPanic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	notifier := NewWebhookNotifier(server.URL, nil)
	// Should not panic
	notifier.Notify()
}

func TestNotify_UnreachableURLDoesNotPanic(t *testing.T) {
	notifier := NewWebhookNotifier("http://127.0.0.1:1", nil)
	// Should not panic
	notifier.Notify()
}
