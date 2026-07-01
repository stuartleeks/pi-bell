package onvif

import (
	"testing"
	"time"
)

func TestParseISODuration(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"PT60S", 60 * time.Second},
		{"PT1M30S", 90 * time.Second},
		{"PT2H", 2 * time.Hour},
		{"P1DT2H", 26 * time.Hour},
		{"PT0.5S", 500 * time.Millisecond},
	}
	for _, tt := range tests {
		got, err := parseISODuration(tt.in)
		if err != nil {
			t.Errorf("parseISODuration(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseISODuration(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestParseISODuration_Invalid(t *testing.T) {
	if _, err := parseISODuration("not-a-duration"); err == nil {
		t.Errorf("expected error for invalid duration")
	}
}

func TestFormatISODuration(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{60 * time.Second, "PT1M"},
		{90 * time.Second, "PT1M30S"},
		{0, "PT0S"},
		{2 * time.Hour, "PT2H"},
	}
	for _, tt := range tests {
		got := formatISODuration(tt.in)
		if got != tt.want {
			t.Errorf("formatISODuration(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
