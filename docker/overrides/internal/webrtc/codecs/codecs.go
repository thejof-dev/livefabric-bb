package codecs

import (
	"strings"

	"github.com/pion/webrtc/v4"
)

type TrackCodeType uint

const (
	AudioTrackLabelDefault = "Audio"
	VideoTrackLabelDefault = "Video"
)
const (
	VideoTrackCodecH264 TrackCodeType = iota + 1
	VideoTrackCodecH265
	VideoTrackCodecVP8
	VideoTrackCodecVP9
	VideoTrackCodecAV1

	audioTrackCodecOpus
)

var videoRTCPFeedback = []webrtc.RTCPFeedback{
	{Type: "goog-remb", Parameter: ""},
	// FIR + PLI let a viewer recover from loss by requesting a fresh keyframe
	// (bounded, one keyframe per request). The generic {Type: "nack"} per-packet
	// retransmission feedback is DELIBERATELY omitted: on a lossy viewer link it
	// drives an unbounded NACK retransmission storm (the server resends its whole
	// recent packet buffer every RTCP interval) because broadcast-box wires no
	// congestion control to throttle it, saturating egress and freezing video.
	// The NACK responder is also removed from the interceptor registry
	// (see internal/webrtc/interceptors/interceptors.go) as a belt-and-braces.
	{Type: "ccm", Parameter: "fir"},
	{Type: "nack", Parameter: "pli"},
}

var videoCodecs = []webrtc.RTPCodecParameters{
	{
		PayloadType: 96,
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:     webrtc.MimeTypeH264,
			ClockRate:    90000,
			SDPFmtpLine:  "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
			RTCPFeedback: videoRTCPFeedback,
		},
	},
	{
		PayloadType: 102,
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:     webrtc.MimeTypeH264,
			ClockRate:    90000,
			SDPFmtpLine:  "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42001f",
			RTCPFeedback: videoRTCPFeedback,
		},
	},
	{
		PayloadType: 103,
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:     webrtc.MimeTypeH264,
			ClockRate:    90000,
			SDPFmtpLine:  "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42001f",
			RTCPFeedback: videoRTCPFeedback,
		},
	},
	{
		PayloadType: 104,
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:     webrtc.MimeTypeH264,
			ClockRate:    90000,
			SDPFmtpLine:  "level-asymmetry-allowed=1;packetization-mode=0;profile-level-id=42001f",
			RTCPFeedback: videoRTCPFeedback,
		},
	},
	{
		PayloadType: 106,
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:     webrtc.MimeTypeH264,
			ClockRate:    90000,
			SDPFmtpLine:  "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
			RTCPFeedback: videoRTCPFeedback,
		},
	},
	{
		PayloadType: 108,
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:     webrtc.MimeTypeH264,
			ClockRate:    90000,
			SDPFmtpLine:  "level-asymmetry-allowed=1;packetization-mode=0;profile-level-id=42e01f",
			RTCPFeedback: videoRTCPFeedback,
		},
	},
	{
		PayloadType: 39,
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:     webrtc.MimeTypeH264,
			ClockRate:    90000,
			SDPFmtpLine:  "level-asymmetry-allowed=1;packetization-mode=0;profile-level-id=4d001f",
			RTCPFeedback: videoRTCPFeedback,
		},
	},
	{
		PayloadType: 45,
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:     webrtc.MimeTypeAV1,
			ClockRate:    90000,
			SDPFmtpLine:  "",
			RTCPFeedback: videoRTCPFeedback,
		},
	},
	{
		PayloadType: 98,
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:     webrtc.MimeTypeVP9,
			ClockRate:    90000,
			SDPFmtpLine:  "profile-id=0",
			RTCPFeedback: videoRTCPFeedback,
		},
	},
	{
		PayloadType: 100,
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:     webrtc.MimeTypeVP9,
			ClockRate:    90000,
			SDPFmtpLine:  "profile-id=2",
			RTCPFeedback: videoRTCPFeedback,
		},
	},
	{
		PayloadType: 113,
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:     webrtc.MimeTypeH265,
			ClockRate:    90000,
			SDPFmtpLine:  "level-id=93;profile-id=1;tier-flag=0;tx-mode=SRST",
			RTCPFeedback: videoRTCPFeedback,
		},
	},
}

var audioCodecs = []webrtc.RTPCodecParameters{
	{
		PayloadType: 111,
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:     webrtc.MimeTypeOpus,
			ClockRate:    48_000,
			Channels:     2,
			SDPFmtpLine:  "minptime=10;useinbandfec=1",
			RTCPFeedback: nil,
		},
	},
}

func GetDefaultTracks(streamKey string) (audioTrack *TrackMultiCodec, videoTrack *TrackMultiCodec) {
	audioTrack = CreateTrackMultiCodec(
		"audio",
		"pion",
		streamKey,
		webrtc.RTPCodecTypeAudio,
		0)

	videoTrack = CreateTrackMultiCodec(
		"video",
		"pion",
		streamKey,
		webrtc.RTPCodecTypeVideo,
		0)

	return audioTrack, videoTrack
}

func GetAudioTrackCodec(codec string) TrackCodeType {
	lowerCase := strings.ToLower(codec)

	switch {
	case strings.Contains(lowerCase, strings.ToLower(webrtc.MimeTypeOpus)):
		return audioTrackCodecOpus
	}

	return 0
}

func GetVideoTrackCodec(codec string) TrackCodeType {
	lowerCase := strings.ToLower(codec)

	switch {
	case strings.Contains(lowerCase, strings.ToLower(webrtc.MimeTypeH264)):
		return VideoTrackCodecH264

	case strings.Contains(lowerCase, strings.ToLower(webrtc.MimeTypeVP8)):
		return VideoTrackCodecVP8

	case strings.Contains(lowerCase, strings.ToLower(webrtc.MimeTypeVP9)):
		return VideoTrackCodecVP9

	case strings.Contains(lowerCase, strings.ToLower(webrtc.MimeTypeAV1)):
		return VideoTrackCodecAV1

	case strings.Contains(lowerCase, strings.ToLower(webrtc.MimeTypeH265)):
		return VideoTrackCodecH265
	}

	return 0
}
