package whep

import (
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/glimesh/broadcast-box/internal/chat"
	"github.com/glimesh/broadcast-box/internal/webrtc/codecs"
	"github.com/pion/webrtc/v4"
)

// Create and start a new WHEP session
func CreateNewWHEP(
	whepSessionID string,
	streamKey string,
	audioTrack *codecs.TrackMultiCodec,
	videoTrack *codecs.TrackMultiCodec,
	peerConnection *webrtc.PeerConnection,
	pliSender func(),
	chatManager *chat.Manager,
) (w *WHEPSession) {
	slog.Debug("WHEPSession.CreateNewWHEP", "whepSessionID", whepSessionID)

	w = &WHEPSession{
		SessionID:               whepSessionID,
		StreamKey:               streamKey,
		AudioTrack:              audioTrack,
		VideoTrack:              videoTrack,
		AudioTimestamp:          5000,
		VideoTimestamp:          5000,
		PeerConnection:          peerConnection,
		pliSender:               pliSender,
		videoBitrateWindowStart: time.Now(),
		ChatManager:             chatManager,
	}

	// Optional frame-aware egress pacer for WHEP session video traffic.
	// Set BB_WHEP_MAX_BPS (bits/sec) to smooth bursty encoder output to a steady
	// rate. Unset or 0 => disabled (lossless immediate passthrough).
	if v := os.Getenv("BB_WHEP_MAX_BPS"); v != "" {
		if bps, err := strconv.ParseUint(v, 10, 64); err == nil && bps > 0 {
			w.pacer = newVideoPacer(bps, w.writeVideoRTP, w.onPacerOverflow)
			w.pacer.start()
			slog.Info("WHEPSession.VideoPacer.Enabled", "streamKey", streamKey, "bps", bps)
		}
	}

	w.AudioLayerCurrent.Store("")
	w.VideoLayerCurrent.Store("")
	w.IsWaitingForKeyframe.Store(true)
	w.IsSessionClosed.Store(false)
	return w
}

// Closes down the WHEP session completely
func (w *WHEPSession) Close() {
	// Close WHEP channels
	w.SessionClose.Do(func() {
		slog.Debug("WHEPSession.Close")
		w.IsSessionClosed.Store(true)

		// Stop the egress pacer goroutine (if enabled)
		if w.pacer != nil {
			w.pacer.close()
		}

		// Close PeerConnection
		slog.Debug("WHEPSession.Close.PeerConnection.GracefulClose")
		err := w.PeerConnection.Close()
		if err != nil {
			slog.Error("WHEPSession.Close.PeerConnection.Error", "err", err)
		}
		slog.Debug("WHEPSession.Close.PeerConnection.GracefulClose.Completed")

		// Empty tracks
		w.AudioLock.Lock()
		w.VideoLock.Lock()

		w.AudioTrack = nil
		w.VideoTrack = nil

		w.VideoLock.Unlock()
		w.AudioLock.Unlock()

		if w.onClose != nil {
			w.onClose(w.SessionID)
		}
	})
}

func (w *WHEPSession) SetOnClose(onClose func(string)) {
	w.onClose = onClose
}

// Get the current status of the WHEP session
func (w *WHEPSession) GetWHEPSessionStatus() (state SessionState) {
	w.AudioLock.RLock()
	w.VideoLock.Lock()
	w.updateVideoBitrateLocked(time.Now())

	currentAudioLayer := w.AudioLayerCurrent.Load().(string)
	currentVideoLayer := w.VideoLayerCurrent.Load().(string)

	state = SessionState{
		ID: w.SessionID,

		AudioLayerCurrent:   currentAudioLayer,
		AudioTimestamp:      w.AudioTimestamp,
		AudioPacketsWritten: w.AudioPacketsWritten,
		AudioSequenceNumber: uint64(w.AudioSequenceNumber),

		VideoLayerCurrent:   currentVideoLayer,
		VideoTimestamp:      w.VideoTimestamp,
		VideoBitrate:        w.VideoBitrate.Load(),
		VideoPacketsWritten: w.VideoPacketsWritten,
		VideoPacketsDropped: w.VideoPacketsDropped.Load(),
		VideoSequenceNumber: uint64(w.VideoSequenceNumber),
	}

	w.VideoLock.Unlock()
	w.AudioLock.RUnlock()

	return
}

// Sets the requested audio layer for this WHEP session.
func (w *WHEPSession) SetAudioLayer(encodingID string) {
	slog.Debug("Setting Audio Layer")
	w.AudioLayerCurrent.Store(encodingID)
	w.IsWaitingForKeyframe.Store(true)
	w.SendPLI()
}

// Sets the requested video layer for this WHEP session.
func (w *WHEPSession) SetVideoLayer(encodingID string) {
	slog.Debug("Setting Video Layer")

	w.VideoLock.Lock()
	w.VideoLayerCurrent.Store(encodingID)
	w.videoLayerPriority = 0
	w.videoLayerExplicit = encodingID != ""
	w.VideoLock.Unlock()

	w.IsWaitingForKeyframe.Store(true)
	w.SendPLI()
}

func (w *WHEPSession) SendPLI() {
	if w.IsSessionClosed.Load() {
		return
	}

	const minPLIInterval = 750 * time.Millisecond
	now := time.Now()

	w.PLILock.Lock()
	if !w.lastPLISent.IsZero() && now.Sub(w.lastPLISent) < minPLIInterval {
		w.PLILock.Unlock()
		return
	}
	w.lastPLISent = now
	w.PLILock.Unlock()

	w.pliSender()
}

// Reset per-publisher delivery state when a new WHIP publisher connects.
func (w *WHEPSession) ResetForNewPublisher() {
	w.VideoLock.Lock()
	defer w.VideoLock.Unlock()

	w.AudioLayerCurrent.Store("")
	w.VideoLayerCurrent.Store("")
	w.videoLayerPriority = 0
	w.videoLayerExplicit = false
	w.IsWaitingForKeyframe.Store(true)
}

func (w *WHEPSession) updateVideoBitrateLocked(now time.Time) {
	if w.videoBitrateWindowStart.IsZero() {
		w.videoBitrateWindowStart = now
		return
	}

	elapsed := now.Sub(w.videoBitrateWindowStart)
	if elapsed < time.Second {
		return
	}

	bytesDiff := w.VideoBytesWritten - w.videoBitrateWindowBytes
	if bytesDiff < 0 {
		bytesDiff = 0
	}

	w.VideoBitrate.Store(uint64(float64(bytesDiff) / elapsed.Seconds()))
	w.videoBitrateWindowStart = now
	w.videoBitrateWindowBytes = w.VideoBytesWritten
}

func (w *WHEPSession) GetVideoLayerOrDefault(defaultLayer string, defaultPriority int) string {
	w.VideoLock.Lock()
	defer w.VideoLock.Unlock()

	currentLayer, _ := w.VideoLayerCurrent.Load().(string)
	if w.videoLayerExplicit {
		return currentLayer
	}

	if currentLayer == "" {
		w.VideoLayerCurrent.Store(defaultLayer)
		w.videoLayerPriority = defaultPriority
		w.IsWaitingForKeyframe.Store(true)
		return defaultLayer
	}

	if currentLayer == defaultLayer {
		w.videoLayerPriority = defaultPriority
		return currentLayer
	}

	// Lower numeric priority value means a better simulcast layer.
	if w.videoLayerPriority == 0 || defaultPriority < w.videoLayerPriority {
		w.VideoLayerCurrent.Store(defaultLayer)
		w.videoLayerPriority = defaultPriority
		w.IsWaitingForKeyframe.Store(true)
		return defaultLayer
	}

	return currentLayer
}
