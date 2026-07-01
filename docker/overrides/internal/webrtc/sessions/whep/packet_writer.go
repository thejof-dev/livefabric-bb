package whep

import (
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/glimesh/broadcast-box/internal/settings"
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

	w.VideoBytesWritten += len(packet.Packet.Payload)
	w.VideoPacketsWritten += 1
	w.VideoSequenceNumber = uint16(w.VideoSequenceNumber) + uint16(packet.SequenceDiff)
	w.VideoTimestamp = uint32(int64(w.VideoTimestamp) + packet.TimeDiff)
	w.updateVideoBitrateLocked(now)
	videoSequenceNumber := w.VideoSequenceNumber
	videoTimestamp := w.VideoTimestamp
	videoTrack := w.VideoTrack
	pacer := w.pacer
	w.VideoLock.Unlock()

	if videoTrack == nil {
		return
	}

	packet.Packet.SequenceNumber = videoSequenceNumber
	packet.Packet.Timestamp = videoTimestamp

	// Pacer inactive (default): forward immediately (lossless passthrough).
	if pacer == nil || !settings.PacerActive() {
		w.writeVideoRTP(packet)
		return
	}

	// Frame-aware pacing. Deep-copy the packet because the write is deferred:
	// the upstream RTP buffer may be reused before the pacer releases it.
	paced := packet
	clonedPacket := *packet.Packet
	payload := make([]byte, len(packet.Packet.Payload))
	copy(payload, packet.Packet.Payload)
	clonedPacket.Payload = payload
	paced.Packet = &clonedPacket

	pacer.enqueue(pacedPacket{
		packet: paced,
		size:   len(payload),
	})
}

// writeVideoRTP performs the actual RTP write to the viewer track, handling a
// dropped connection. It is called both on the synchronous passthrough path and
// from the pacer's drain goroutine.
func (w *WHEPSession) writeVideoRTP(packet codecs.TrackPacket) {
	w.VideoLock.RLock()
	videoTrack := w.VideoTrack
	w.VideoLock.RUnlock()

	if videoTrack == nil {
		return
	}

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

// onPacerOverflow is invoked when the pacing queue overflows under sustained
// overload. Pending frames are already flushed by the pacer; here we account
// for the loss and force a clean resync on the next keyframe so the decoder is
// never fed a partial frame.
func (w *WHEPSession) onPacerOverflow(dropped uint64) {
	if dropped > 0 {
		w.VideoPacketsDropped.Add(dropped)
	}
	w.IsWaitingForKeyframe.Store(true)
	w.SendPLI()
}
