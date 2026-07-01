package whep

import (
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/glimesh/broadcast-box/internal/webrtc/codecs"
)

// Sends provided audio packet to the WHEP session
func (w *WHEPSession) SendAudioPacket(packet codecs.TrackPacket) {
	if w.IsSessionClosed.Load() {
		return
	}

	w.AudioLock.Lock()
	if w.AudioTrack == nil {
		w.AudioLock.Unlock()
		return
	}

	w.AudioPacketsWritten += 1
	w.AudioTimestamp = uint32(int64(w.AudioTimestamp) + packet.TimeDiff)
	audioTrack := w.AudioTrack
	w.AudioLock.Unlock()

	if err := audioTrack.WriteRTP(packet.Packet, packet.Codec); err != nil {
		if errors.Is(err, io.ErrClosedPipe) {
			slog.Info("WHEPSession.SendAudioPacket.ConnectionDropped")
			w.Close()
		} else {
			slog.Error("WHEPSession.SendAudioPacket.Error", "err", err)
		}
	}
}

// Sends provided video packet to the WHEP session
func (w *WHEPSession) SendVideoPacket(packet codecs.TrackPacket) {
	if w.IsSessionClosed.Load() {
		return
	}

	if w.IsWaitingForKeyframe.Load() {
		if !packet.IsKeyframe {
			w.SendPLI()
			return
		}

		w.IsWaitingForKeyframe.Store(false)
	}

	w.VideoLock.Lock()
	now := time.Now()

	// Optional hard limiter: clamp per-session egress to BB_WHEP_MAX_BPS.
	limitBps := w.VideoRateLimitBps.Load()
	if limitBps > 0 {
		if w.videoRateWindowStart.IsZero() {
			w.videoRateWindowStart = now
		}

		elapsed := now.Sub(w.videoRateWindowStart)
		if elapsed >= time.Second {
			w.videoRateWindowStart = now
			w.videoRateWindowBytes = 0
			elapsed = 0
		}

		limitBytesPerSec := int(limitBps / 8)
		nextBytes := w.videoRateWindowBytes + len(packet.Packet.Payload)
		if nextBytes > limitBytesPerSec {
			w.VideoPacketsDropped.Add(1)
			w.VideoLock.Unlock()
			return
		}

		w.videoRateWindowBytes = nextBytes
	}

	w.VideoBytesWritten += len(packet.Packet.Payload)
	w.VideoPacketsWritten += 1
	w.VideoSequenceNumber = uint16(w.VideoSequenceNumber) + uint16(packet.SequenceDiff)
	w.VideoTimestamp = uint32(int64(w.VideoTimestamp) + packet.TimeDiff)
	w.updateVideoBitrateLocked(now)
	videoSequenceNumber := w.VideoSequenceNumber
	videoTimestamp := w.VideoTimestamp
	videoTrack := w.VideoTrack
	w.VideoLock.Unlock()

	if videoTrack == nil {
		return
	}

	packet.Packet.SequenceNumber = videoSequenceNumber
	packet.Packet.Timestamp = videoTimestamp

	if err := videoTrack.WriteRTP(packet.Packet, packet.Codec); err != nil {
		w.VideoPacketsDropped.Add(1)

		if errors.Is(err, io.ErrClosedPipe) {
			slog.Info("WHEPSession.SendVideoPacket.ConnectionDropped")
			w.Close()
		} else {
			slog.Error("WHEPSession.SendVideoPacket.Error", "err", err)
		}
	}
}
