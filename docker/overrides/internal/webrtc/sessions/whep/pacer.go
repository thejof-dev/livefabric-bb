package whep

import (
	"sync"
	"time"

	"github.com/glimesh/broadcast-box/internal/webrtc/codecs"
)

// pacedPacket is a fully-finalized RTP packet (sequence number and timestamp
// already rewritten, payload deep-copied) waiting to be released onto the
// viewer track.
type pacedPacket struct {
	packet codecs.TrackPacket
	size   int
}

// videoPacer is a per-session leaky-bucket pacer. It smooths bursty encoder
// output (large keyframes, VBR spikes) into a steady egress stream using a
// token bucket, absorbing transient bursts in a bounded queue instead of
// dropping packets. This is lossless for short bursts and only adds a small,
// bounded amount of latency.
//
// It is frame-safe: in steady state every packet of every frame is delivered
// in order, so a decoder never sees a partial frame. Under sustained overload
// the queue overflows; the pacer then drops the whole pending queue and asks
// the session to resync on the next keyframe, rather than slicing individual
// packets out of a frame (which would corrupt the H.264 reference chain).
//
// The target rate is read live from rateFn on every release, so runtime changes
// via the settings page take effect immediately without recreating the pacer.
type videoPacer struct {
	mu   sync.Mutex
	cond *sync.Cond

	queue      []pacedPacket
	queueBytes int

	tokens     float64
	lastRefill time.Time

	closed bool

	rateFn     func() uint64 // current target bits/sec (0 => unlimited)
	write      func(codecs.TrackPacket)
	onOverflow func(dropped uint64)
}

func newVideoPacer(rateFn func() uint64, write func(codecs.TrackPacket), onOverflow func(uint64)) *videoPacer {
	p := &videoPacer{
		rateFn:     rateFn,
		write:      write,
		onOverflow: onOverflow,
	}
	p.cond = sync.NewCond(&p.mu)
	return p
}

func (p *videoPacer) start() {
	go p.run()
}

// rateBytesPerSec returns the current target rate in bytes/sec (0 = unlimited).
func (p *videoPacer) rateBytesPerSec() float64 {
	if p.rateFn == nil {
		return 0
	}
	return float64(p.rateFn()) / 8.0
}

// burstBytes is the token-bucket capacity: allow a short instantaneous burst
// (~250ms of rate, at least one keyframe) so keyframes are not needlessly held.
func burstBytes(rate float64) float64 {
	b := rate * 0.25
	if b < 64*1024 {
		b = 64 * 1024
	}
	return b
}

// maxQueueBytes is how much bursty output is buffered before declaring
// sustained overload (~1s of rate, floor 256KB).
func maxQueueBytes(rate float64) int {
	m := int(rate)
	if m < 256*1024 {
		m = 256 * 1024
	}
	return m
}

// enqueue admits a finalized packet into the pacing queue. On sustained
// overload it flushes the pending queue and signals the session to resync,
// guaranteeing that only whole frames are ever released downstream.
func (p *videoPacer) enqueue(pk pacedPacket) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}

	limit := maxQueueBytes(p.rateBytesPerSec())
	if p.queueBytes+pk.size > limit {
		dropped := uint64(len(p.queue)) + 1
		p.queue = nil
		p.queueBytes = 0
		p.mu.Unlock()

		if p.onOverflow != nil {
			p.onOverflow(dropped)
		}
		return
	}

	p.queue = append(p.queue, pk)
	p.queueBytes += pk.size
	p.cond.Signal()
	p.mu.Unlock()
}

func (p *videoPacer) close() {
	p.mu.Lock()
	p.closed = true
	p.queue = nil
	p.queueBytes = 0
	p.cond.Broadcast()
	p.mu.Unlock()
}

func (p *videoPacer) refillLocked(now time.Time, rate float64) {
	if p.lastRefill.IsZero() {
		p.lastRefill = now
		return
	}

	elapsed := now.Sub(p.lastRefill).Seconds()
	if elapsed <= 0 {
		return
	}

	p.tokens += elapsed * rate
	if maxTokens := burstBytes(rate); p.tokens > maxTokens {
		p.tokens = maxTokens
	}
	p.lastRefill = now
}

func (p *videoPacer) run() {
	for {
		p.mu.Lock()
		for !p.closed && len(p.queue) == 0 {
			p.cond.Wait()
		}
		if p.closed {
			p.mu.Unlock()
			return
		}

		rate := p.rateBytesPerSec()
		head := p.queue[0]

		// Unlimited (pacer effectively disabled): release immediately.
		if rate <= 0 {
			p.queue = p.queue[1:]
			if len(p.queue) == 0 {
				p.queue = nil
			}
			p.queueBytes -= head.size
			write := p.write
			p.mu.Unlock()
			if write != nil {
				write(head.packet)
			}
			continue
		}

		p.refillLocked(time.Now(), rate)

		// Not enough budget yet: sleep for the deficit, then re-evaluate.
		if p.tokens < float64(head.size) {
			deficit := float64(head.size) - p.tokens
			wait := time.Duration(deficit / rate * float64(time.Second))
			if wait < time.Millisecond {
				wait = time.Millisecond
			}
			p.mu.Unlock()
			time.Sleep(wait)
			continue
		}

		p.tokens -= float64(head.size)
		p.queue = p.queue[1:]
		if len(p.queue) == 0 {
			p.queue = nil
		}
		p.queueBytes -= head.size
		write := p.write
		p.mu.Unlock()

		if write != nil {
			write(head.packet)
		}
	}
}
