package interceptors

import (
	"log/slog"
	"os"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/nack"
	"github.com/pion/webrtc/v4"
)

// nackResponderSize bounds the NACK responder's retransmit ring buffer, in
// packets. It MUST be a power of two (Pion validates against the set
// 1..32768). This is the key safety lever against the retransmission storm
// described below: the responder can never resend more than this many packets
// in a burst, so worst-case retransmit volume is capped regardless of how
// aggressively a viewer NACKs.
//
// Egress is ~150 small (~150-byte) RTP packets/sec, so 256 packets is ~1.7s of
// send history — comfortably longer than any realistic viewer RTT (the packet
// must still be buffered when the NACK arrives) and longer than the ~2s
// keyframe interval's worth of loss we need to repair, while a full-buffer
// resend burst is only ~256*150*8 ≈ 0.3 Mbit. The upstream default (1024) gives
// ~6.8s of history and a 4x larger worst-case burst for no benefit here.
const nackResponderSize = 256

// GetRegistry builds the interceptor registry with a BOUNDED NACK responder.
//
// Background: upstream calls webrtc.RegisterDefaultInterceptors, whose
// ConfigureNack uses the DEFAULT (1024-packet) responder buffer. The responder
// buffers outgoing RTP and retransmits it whenever a viewer sends an RTCP NACK.
// On a lossy or delayed viewer link the viewer NACKs its entire un-received
// window on every RTCP feedback interval (~100ms); because broadcast-box wires
// no congestion control (no GCC/BWE, no transport-layer send pacing) to
// throttle those retransmits, the server can resend the whole recent buffer
// every interval — a positive-feedback retransmission storm that saturates
// egress and freezes video for every viewer.
//
// These nodes always sit on uncontrolled WAN behind SpeedFusion, so the final
// box->viewer WebRTC hop is lossy and NACK repair is required to avoid frame
// freezes (a single lost packet otherwise waits up to a full keyframe interval
// ~2s to recover). We therefore keep NACK, but bound the responder buffer via
// ResponderSize(nackResponderSize) so it can never storm: the worst-case resend
// burst is fixed and tiny. PLI/FIR remain as the large-loss fallback. All other
// default interceptors — RTCP reports, simulcast header extensions, stats, and
// the TWCC sender — are preserved so /api/status and monitoring keep working.
func GetRegistry(mediaEngine *webrtc.MediaEngine) interceptor.Registry {
	interceptorRegistry := &interceptor.Registry{}

	if err := webrtc.ConfigureNackWithOptions(mediaEngine, interceptorRegistry, nil, nack.ResponderSize(nackResponderSize)); err != nil {
		slog.Error("Failed to configure bounded NACK", "err", err)
		os.Exit(1)
	}

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
