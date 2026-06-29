package rtspserver

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
	"github.com/bluenviron/gortsplib/v4/pkg/format/rtpmjpeg"
)

// mjpegClockRate is the RTP clock rate for MJPEG (RFC 2435), in Hz.
const mjpegClockRate = 90000

// RTSPServer wraps a gortsplib.Server to stream MJPEG frames over RTSP.
type RTSPServer struct {
	server *gortsplib.Server
	stream *gortsplib.ServerStream
	desc   *description.Session
	media  *description.Media

	mu      sync.Mutex
	encoder *rtpmjpeg.Encoder
	started bool

	// Timestamp tracking. The rtpmjpeg encoder leaves the RTP timestamp at
	// zero, so we must populate it ourselves. MJPEG uses a 90 kHz clock; the
	// timestamp for each frame is derived from the elapsed time since the
	// first published frame, offset by a random initial value.
	initialTimestamp uint32
	startTime        time.Time
	haveStartTime    bool
}

// NewRTSPServer creates a new RTSP server listening on the given port.
func NewRTSPServer(port int) *RTSPServer {
	rs := &RTSPServer{}

	mjpegFormat := &format.MJPEG{}
	rs.media = &description.Media{
		Type:    description.MediaTypeVideo,
		Formats: []format.Format{mjpegFormat},
	}

	rs.desc = &description.Session{
		Medias: []*description.Media{rs.media},
	}

	rs.server = &gortsplib.Server{
		Handler:     rs,
		RTSPAddress: fmt.Sprintf(":%d", port),
	}

	return rs
}

// Start starts the RTSP server in a background goroutine.
func (rs *RTSPServer) Start() error {
	enc, err := (&format.MJPEG{}).CreateEncoder()
	if err != nil {
		return fmt.Errorf("failed to create MJPEG RTP encoder: %w", err)
	}
	rs.encoder = enc

	// Pick a random initial RTP timestamp, as recommended by RFC 3550.
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Errorf("failed to generate initial RTP timestamp: %w", err)
	}
	rs.initialTimestamp = binary.BigEndian.Uint32(b[:])

	err = rs.server.Start()
	if err != nil {
		return fmt.Errorf("failed to start RTSP server: %w", err)
	}

	// Create the stream after the server has started so that gortsplib has
	// applied its default configuration (e.g. the RTCP sender report period).
	// NewServerStream spawns an RTCP sender goroutine that calls
	// time.NewTicker with the server's senderReportPeriod, which is only
	// defaulted to a non-zero value inside server.Start(). Creating the
	// stream beforehand would pass a zero interval and panic.
	rs.stream = gortsplib.NewServerStream(rs.server, rs.desc)
	rs.started = true

	return nil
}

// Stop gracefully shuts down the RTSP server.
func (rs *RTSPServer) Stop() {
	if !rs.started {
		// Start() was never called (or it failed), so there is nothing to
		// shut down. Calling server.Close() here would dereference a nil
		// cancel func and panic.
		return
	}
	if rs.stream != nil {
		rs.stream.Close()
	}
	rs.server.Close()
}

// PublishFrame encodes a JPEG frame into RTP packets and writes them
// to all connected RTSP clients. Safe to call from any goroutine.
func (rs *RTSPServer) PublishFrame(jpegData []byte) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if rs.encoder == nil {
		return
	}

	packets, err := rs.encoder.Encode(jpegData)
	if err != nil {
		// Not all JPEG images are compatible with RFC 2435.
		// Silently drop incompatible frames.
		return
	}

	// The encoder does not set the RTP timestamp, so derive one from the
	// elapsed wall-clock time using the 90 kHz MJPEG clock. All packets that
	// make up a single frame share the same timestamp; it advances between
	// frames so clients (e.g. ffmpeg) see monotonically increasing timestamps.
	now := time.Now()
	if !rs.haveStartTime {
		rs.startTime = now
		rs.haveStartTime = true
	}
	elapsed := now.Sub(rs.startTime)
	timestamp := rs.initialTimestamp + uint32(elapsed.Seconds()*mjpegClockRate)

	for _, pkt := range packets {
		pkt.Timestamp = timestamp
		err = rs.stream.WritePacketRTP(rs.media, pkt)
		if err != nil {
			return
		}
	}
}

// --- gortsplib ServerHandler interface implementations ---

func (rs *RTSPServer) OnConnOpen(_ *gortsplib.ServerHandlerOnConnOpenCtx) {
	log.Println("RTSP: client connected")
}

func (rs *RTSPServer) OnConnClose(ctx *gortsplib.ServerHandlerOnConnCloseCtx) {
	log.Printf("RTSP: client disconnected (%v)", ctx.Error)
}

func (rs *RTSPServer) OnSessionOpen(_ *gortsplib.ServerHandlerOnSessionOpenCtx) {
	log.Println("RTSP: session opened")
}

func (rs *RTSPServer) OnSessionClose(_ *gortsplib.ServerHandlerOnSessionCloseCtx) {
	log.Println("RTSP: session closed")
}

func (rs *RTSPServer) OnDescribe(_ *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	return &base.Response{
		StatusCode: base.StatusOK,
	}, rs.stream, nil
}

func (rs *RTSPServer) OnSetup(_ *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	return &base.Response{
		StatusCode: base.StatusOK,
	}, rs.stream, nil
}

func (rs *RTSPServer) OnPlay(_ *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	return &base.Response{
		StatusCode: base.StatusOK,
	}, nil
}

// We don't accept external publishers.
func (rs *RTSPServer) OnAnnounce(_ *gortsplib.ServerHandlerOnAnnounceCtx) (*base.Response, error) {
	return &base.Response{
		StatusCode: base.StatusMethodNotAllowed,
	}, nil
}

func (rs *RTSPServer) OnRecord(_ *gortsplib.ServerHandlerOnRecordCtx) (*base.Response, error) {
	return &base.Response{
		StatusCode: base.StatusMethodNotAllowed,
	}, nil
}
