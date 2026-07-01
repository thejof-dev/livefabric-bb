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
type videoPacer struct {
	mu   sync.Mutex
	cond *sync.Cond

	queue      []pacedPacket
	queueBytes int

	rateBytesPerSec float64
	bucketBytes     float64
	tokens          float64
	lastRefill      time.Time
	maxQueueBytes   int

	closed bool

	write      func(codecs.TrackPacket)
	onOverflow func(dropped uint64)
}

func newVideoPacer(bitsPerSec uint64, write func(codecs.TrackPacket), onOverflow func(uint64)) *videoPacer {
	rate := float64(bitsPerSec) / 8.0

	// Allow a short instantaneous burst (~250ms of rate, at least one keyframe)
	// so keyframes are not needlessly delayed.
	burst := rate * 0.25
	if burst < 64*1024 {
		burst = 64 * 1024
	}

	// Absorb up to ~1s of bursty output before declaring sustained overload.
	maxQueue := int(rate)
	if maxQueue < 256*1024 {
		maxQueue = 256 * 1024
	}

	p := &videoPacer{
		rateBytesPerSec: rate,
		bucketBytes:     burst,
		tokens:          burst,
		maxQueueBytes:   maxQueue,
		write:           write,
		onOverflow:      onOverflow,
	}
	p.cond = sync.NewCond(&p.mu)
	return p
}

func (p *videoPacer) start() {
	go p.run()
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

	if p.queueBytes+pk.size > p.maxQueueBytes {
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

func (p *videoPacer) refillLocked(now time.Time) {
	if p.lastRefill.IsZero() {
		p.lastRefill = now
		return
	}

	elapsed := now.Sub(p.lastRefill).Seconds()
	if elapsed <= 0 {
		return
	}

	p.tokens += elapsed * p.rateBytesPerSec
	if p.tokens > p.bucketBytes {
		p.tokens = p.bucketBytes
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

		p.refillLocked(time.Now())
		head := p.queue[0]

		// Not enough budget yet: sleep for the deficit, then re-evaluate.
		if p.tokens < float64(head.size) {
			deficit := float64(head.size) - p.tokens
			wait := time.Duration(deficit / p.rateBytesPerSec * float64(time.Second))
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
