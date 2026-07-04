package interceptors

import (
	"log/slog"
	"os"

	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
)

// flexFEC03PayloadType is the dynamic RTP payload type advertised for the
// FlexFEC-03 repair stream. It must not collide with any media payload type
// registered in codecs.go (96,102,103,104,106,108,39,45,98,100,113,111).
const flexFEC03PayloadType webrtc.PayloadType = 49

// GetRegistry builds the interceptor registry WITHOUT the NACK interceptor, but
// WITH a FlexFEC-03 forward-error-correction generator.
//
// Why not NACK: upstream's ConfigureNack adds a NACK *responder* that retransmits
// buffered RTP whenever a viewer NACKs. On a lossy/delayed viewer link the viewer
// NACKs its whole un-received window every RTCP interval (~100ms); broadcast-box
// wires no congestion control to throttle those retransmits, so the server resends
// its recent buffer every interval — an unbounded retransmission storm that
// saturates egress. Bounding the responder ring buffer (ResponderSize) did NOT
// tame it in production, so NACK stays off the WHEP egress path entirely.
//
// Why FlexFEC instead: these nodes always run on uncontrolled WAN behind
// SpeedFusion, so the final box->viewer hop drops/reorders packets, and without
// per-packet repair a single loss freezes video until the next keyframe (~1s GOP).
// FlexFEC adds a FIXED-overhead forward repair stream (default 2 FEC per 5 media
// packets), so — unlike NACK — its bandwidth is bounded and has NO feedback loop
// that can storm. The generator is a no-op unless the viewer negotiates the
// FlexFEC-03 codec (it only emits when the stream has an FEC payload type + SSRC),
// so a browser that doesn't support FlexFEC simply behaves like the no-NACK build.
//
// PLI/FIR remain (codecs.go) as the large-loss fallback. All other default
// interceptors — RTCP reports, simulcast header extensions, stats, and the TWCC
// sender — are preserved so /api/status and monitoring keep working.
func GetRegistry(mediaEngine *webrtc.MediaEngine) interceptor.Registry {
	interceptorRegistry := &interceptor.Registry{}

	if err := webrtc.ConfigureFlexFEC03(flexFEC03PayloadType, mediaEngine, interceptorRegistry); err != nil {
		slog.Error("Failed to configure FlexFEC-03", "err", err)
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
