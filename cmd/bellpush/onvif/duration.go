package onvif

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// reISODuration matches the (simple, non-negative) subset of ISO-8601 durations
// that ONVIF clients use for Timeout/TerminationTime values, e.g. "PT60S",
// "PT1M30S", "PT2H", "P1DT2H". Year/month components are deliberately not
// supported (not meaningful for subscription timeouts and ambiguous in length).
var reISODuration = regexp.MustCompile(`^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+(?:\.\d+)?)S)?)?$`)

// parseISODuration parses a subset of ISO-8601 durations into a time.Duration.
func parseISODuration(s string) (time.Duration, error) {
	m := reISODuration.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("invalid ISO-8601 duration: %q", s)
	}
	var d time.Duration
	if m[1] != "" {
		days, _ := strconv.Atoi(m[1])
		d += time.Duration(days) * 24 * time.Hour
	}
	if m[2] != "" {
		hours, _ := strconv.Atoi(m[2])
		d += time.Duration(hours) * time.Hour
	}
	if m[3] != "" {
		minutes, _ := strconv.Atoi(m[3])
		d += time.Duration(minutes) * time.Minute
	}
	if m[4] != "" {
		seconds, _ := strconv.ParseFloat(m[4], 64)
		d += time.Duration(seconds * float64(time.Second))
	}
	return d, nil
}

// formatISODuration formats a (non-negative) time.Duration as an ISO-8601 duration,
// e.g. "PT1M30S".
func formatISODuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalSeconds := int64(d.Seconds())
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60

	s := "PT"
	if hours > 0 {
		s += fmt.Sprintf("%dH", hours)
	}
	if minutes > 0 {
		s += fmt.Sprintf("%dM", minutes)
	}
	if seconds > 0 || (hours == 0 && minutes == 0) {
		s += fmt.Sprintf("%dS", seconds)
	}
	return s
}
