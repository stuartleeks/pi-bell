package rtspserver

import (
	"fmt"
	"net"
	"testing"
	"time"
)

// freePort asks the OS for a free TCP port and returns it.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find a free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// TestStartDoesNotPanic verifies that starting the RTSP server does not panic.
//
// This is a regression test for a bug where the ServerStream (and its RTCP
// sender goroutine) was created before server.Start() had applied gortsplib's
// default sender-report period. That left the period at 0, causing the RTCP
// sender goroutine to call time.NewTicker(0) and panic with
// "non-positive interval for NewTicker". Because the panic happened in a
// background goroutine, it crashed the whole process.
//
// An unrecovered panic in any goroutine terminates the test binary, so simply
// starting the server and giving the RTCP goroutine a moment to spin up is
// enough to catch the regression.
func TestStartDoesNotPanic(t *testing.T) {
	rs := NewRTSPServer(freePort(t))

	if err := rs.Start(); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	defer rs.Stop()

	// Give the RTCP sender goroutine time to run time.NewTicker. With the bug
	// present, this window is when the panic fires.
	time.Sleep(200 * time.Millisecond)
}

// TestStreamCreatedAfterStart verifies that the stream is available once the
// server has started, so DESCRIBE/SETUP handlers can serve it.
func TestStreamCreatedAfterStart(t *testing.T) {
	rs := NewRTSPServer(freePort(t))

	if rs.stream != nil {
		t.Fatal("stream should not exist before Start()")
	}

	if err := rs.Start(); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	defer rs.Stop()

	if rs.stream == nil {
		t.Fatal("stream should be created after Start()")
	}
}

// TestStopWithoutStart ensures Stop() is safe to call when Start() was never
// invoked (stream is nil).
func TestStopWithoutStart(t *testing.T) {
	rs := NewRTSPServer(freePort(t))
	// Should not panic even though Start() was never called.
	rs.Stop()
}

// TestStartListensOnPort verifies the server actually accepts TCP connections
// on the configured RTSP port after Start().
func TestStartListensOnPort(t *testing.T) {
	port := freePort(t)
	rs := NewRTSPServer(port)

	if err := rs.Start(); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	defer rs.Stop()

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		t.Fatalf("expected RTSP server to be listening on port %d: %v", port, err)
	}
	conn.Close()
}

// TestPublishFrameBeforeStart ensures publishing a frame before Start() is a
// safe no-op (encoder is nil).
func TestPublishFrameBeforeStart(t *testing.T) {
	rs := NewRTSPServer(freePort(t))
	// Should not panic; encoder is nil so the frame is dropped.
	rs.PublishFrame([]byte{0xFF, 0xD8, 0xFF, 0xD9})
}
