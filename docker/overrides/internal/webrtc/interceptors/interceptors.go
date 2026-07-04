package interceptors

import (
	"log/slog"
	"os"

	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
)

// GetRegistry builds the interceptor registry WITHOUT the NACK interceptor.
//
// Upstream calls webrtc.RegisterDefaultInterceptors, which includes
// ConfigureNack -> the NACK *responder*. The responder buffers outgoing RTP and
// retransmits it whenever a viewer sends an RTCP NACK. On a lossy or delayed
// viewer link the viewer NACKs its entire un-received window on every RTCP
// feedback interval (~100ms); because broadcast-box wires no congestion control
// (no GCC/BWE, no send pacing at the transport layer) to throttle those
// retransmits, the server resends the whole recent buffer every interval. The
// retransmits add congestion, causing more loss, causing more NACKs — an
// unbounded positive-feedback retransmission storm that saturates egress and
// freezes video for every viewer.
//
// We omit NACK entirely and rely on PLI/FIR -> keyframe recovery instead
// (negotiated in codecs.go and driven server-side). All other default
// interceptors — RTCP reports, simulcast header extensions, stats, and the
// TWCC sender — are preserved so /api/status and monitoring keep working.
func GetRegistry(mediaEngine *webrtc.MediaEngine) interceptor.Registry {
	interceptorRegistry := &interceptor.Registry{}

	if err := webrtc.ConfigureRTCPReports(interceptorRegistry); err != nil {
		slog.Error("Failed to configure RTCP reports", "err", err)
		os.Exit(1)
	}

	if err := webrtc.ConfigureSimulcastExtensionHeaders(mediaEngine); err != nil {
		slog.Error("Failed to configure simulcast extension headers", "err", err)
		os.Exit(1)
	}

	if err := webrtc.ConfigureStatsInterceptor(interceptorRegistry); err != nil {
		slog.Error("Failed to configure stats interceptor", "err", err)
		os.Exit(1)
	}

	if err := webrtc.ConfigureTWCCSender(mediaEngine, interceptorRegistry); err != nil {
		slog.Error("Failed to configure TWCC sender", "err", err)
		os.Exit(1)
	}

	return *interceptorRegistry
}
