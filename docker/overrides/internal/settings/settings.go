// Package settings holds the runtime-adjustable LiveFabric-BB configuration
// (egress pacer rate/enable, PLI throttle, NAT/IP override). Values are seeded
// from environment variables, overridden by a persisted JSON file, and can be
// updated live via the /api/settings HTTP endpoint.
//
// It is a leaf package (no imports of other broadcast-box packages) so it can
// be safely used from both the HTTP handlers and the WebRTC session code.
package settings

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Settings is the JSON-serialisable runtime configuration.
type Settings struct {
	PacerEnabled  bool   `json:"pacerEnabled"`
	PacerBps      uint64 `json:"pacerBps"`
	PLIThrottleMs uint64 `json:"pliThrottleMs"`
	NATOverrideIP string `json:"natOverrideIp"`
	SSHEnabled    bool   `json:"sshEnabled"`
	SSHPort       uint64 `json:"sshPort"`
}

const (
	defaultPLIThrottleMs = 750
	minPLIThrottleMs     = 100
	maxPLIThrottleMs     = 5000
	minPacerBps          = 100_000
	maxPacerBps          = 100_000_000
	defaultSSHPort       = 22
)

var (
	once sync.Once
	mu   sync.RWMutex
	cur  Settings
	file string

	aPacerEnabled  atomic.Bool
	aPacerBps      atomic.Uint64
	aPLIThrottleMs atomic.Uint64
)

func filePath() string {
	if p := os.Getenv("BB_SETTINGS_PATH"); p != "" {
		return p
	}
	if p := os.Getenv("STREAM_PROFILE_PATH"); p != "" {
		return strings.TrimRight(p, "/") + "/livefabric-settings.json"
	}
	return "/opt/livefabric-bb/profiles/livefabric-settings.json"
}

// Init loads settings from env + persisted file. Safe to call repeatedly; only
// the first call does work.
func Init() {
	once.Do(func() {
		file = filePath()
		s := seedFromEnv()

		if b, err := os.ReadFile(file); err == nil {
			// Unmarshal onto the seeded defaults so keys absent from an older
			// settings file (e.g. sshEnabled) keep their default instead of
			// silently becoming the zero value.
			fromFile := s
			if json.Unmarshal(b, &fromFile) == nil {
				s = sanitize(fromFile)
			}
		}

		apply(s)
		_ = persist(s) // ensure the file exists for the entrypoint to read
	})
}

func seedFromEnv() Settings {
	// SSH defaults ON; disable only via an explicit falsey SSH_ENABLED.
	s := Settings{PLIThrottleMs: defaultPLIThrottleMs, SSHEnabled: true, SSHPort: defaultSSHPort}

	if v := os.Getenv("SSH_ENABLED"); v != "" {
		s.SSHEnabled = !isFalsey(v)
	}
	if v := os.Getenv("SSH_PORT"); v != "" {
		if p, err := strconv.ParseUint(v, 10, 32); err == nil && p > 0 {
			s.SSHPort = p
		}
	}

	if v := os.Getenv("BB_WHEP_MAX_BPS"); v != "" {
		if bps, err := strconv.ParseUint(v, 10, 64); err == nil && bps > 0 {
			s.PacerBps = bps
			s.PacerEnabled = true
		}
	}
	if v := os.Getenv("BB_PLI_THROTTLE_MS"); v != "" {
		if ms, err := strconv.ParseUint(v, 10, 64); err == nil && ms > 0 {
			s.PLIThrottleMs = ms
		}
	}
	if v := os.Getenv("NAT_1_TO_1_IP"); v != "" {
		s.NATOverrideIP = v
	}

	return sanitize(s)
}

func sanitize(s Settings) Settings {
	if s.PLIThrottleMs == 0 {
		s.PLIThrottleMs = defaultPLIThrottleMs
	}
	if s.PLIThrottleMs < minPLIThrottleMs {
		s.PLIThrottleMs = minPLIThrottleMs
	}
	if s.PLIThrottleMs > maxPLIThrottleMs {
		s.PLIThrottleMs = maxPLIThrottleMs
	}

	if s.PacerEnabled && s.PacerBps > 0 {
		if s.PacerBps < minPacerBps {
			s.PacerBps = minPacerBps
		}
		if s.PacerBps > maxPacerBps {
			s.PacerBps = maxPacerBps
		}
	} else {
		s.PacerEnabled = false
	}

	s.NATOverrideIP = strings.TrimSpace(s.NATOverrideIP)

	if s.SSHPort == 0 || s.SSHPort > 65535 {
		s.SSHPort = defaultSSHPort
	}

	return s
}

// isFalsey reports whether an env string means "off".
func isFalsey(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return true
	}
	return false
}

func apply(s Settings) {
	mu.Lock()
	cur = s
	mu.Unlock()

	aPacerEnabled.Store(s.PacerEnabled)
	aPacerBps.Store(s.PacerBps)
	aPLIThrottleMs.Store(s.PLIThrottleMs)
}

func persist(s Settings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// Get returns a snapshot of the current settings.
func Get() Settings {
	Init()
	mu.RLock()
	defer mu.RUnlock()
	return cur
}

// Update validates, applies (live), and persists new settings, returning the
// sanitised result that was actually stored.
func Update(s Settings) (Settings, error) {
	Init()
	s = sanitize(s)
	apply(s)
	err := persist(s)
	return s, err
}

// PacerActive reports whether the egress pacer should shape video traffic.
// Hot path (called per video packet) — pure atomic reads.
func PacerActive() bool {
	return aPacerEnabled.Load() && aPacerBps.Load() > 0
}

// PacerBps returns the current target pacing rate in bits/sec (0 = unlimited).
func PacerBps() uint64 {
	return aPacerBps.Load()
}

// PLIThrottle returns the minimum interval between forwarded keyframe requests.
func PLIThrottle() time.Duration {
	ms := aPLIThrottleMs.Load()
	if ms == 0 {
		ms = defaultPLIThrottleMs
	}
	return time.Duration(ms) * time.Millisecond
}
