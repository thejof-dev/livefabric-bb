# syntax=docker/dockerfile:1.7

FROM node:22-bookworm-slim AS node

FROM golang:1.25-bookworm AS builder

ARG BROADCAST_BOX_REF=main
WORKDIR /build

ARG TARGETOS
ARG TARGETARCH

COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -sf /usr/local/lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
 && ln -sf /usr/local/lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx

RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    perl \
    ca-certificates \
  && rm -rf /var/lib/apt/lists/*

RUN git clone --depth 1 --branch "${BROADCAST_BOX_REF}" https://github.com/glimesh/broadcast-box.git src

WORKDIR /build/src

COPY docker/overrides/internal/webrtc/codecs/codecs.go /build/src/internal/webrtc/codecs/codecs.go
COPY docker/overrides/internal/webrtc/sessions/whep/types.go /build/src/internal/webrtc/sessions/whep/types.go
COPY docker/overrides/internal/webrtc/sessions/whep/whep.go /build/src/internal/webrtc/sessions/whep/whep.go
COPY docker/overrides/internal/webrtc/sessions/whep/packet_writer.go /build/src/internal/webrtc/sessions/whep/packet_writer.go
COPY docker/overrides/internal/webrtc/sessions/whep/pacer.go /build/src/internal/webrtc/sessions/whep/pacer.go
COPY docker/overrides/internal/settings/settings.go /build/src/internal/settings/settings.go
COPY docker/overrides/internal/server/handlers/settings_handler.go /build/src/internal/server/handlers/settings_handler.go
COPY docker/overrides/internal/server/handlers/monitor_handler.go /build/src/internal/server/handlers/monitor_handler.go

# Register the /settings, /api/settings, /monitor and /api/netstat routes by
# patching the router. The grep guard fails the build loudly if the upstream
# anchor ever moves.
RUN perl -0pi -e 's{(serverMux\.HandleFunc\("/api/status",[^\n]*\n)}{$1\tserverMux.HandleFunc("/settings", corsHandler(settingsPageHandler))\n\tserverMux.HandleFunc("/api/settings", corsHandler(settingsAPIHandler))\n\tserverMux.HandleFunc("/monitor", corsHandler(monitorPageHandler))\n\tserverMux.HandleFunc("/api/netstat", corsHandler(monitorAPIHandler))\n}' internal/server/handlers/routes.go \
 && grep -q '/api/settings' internal/server/handlers/routes.go \
 && grep -q '/api/netstat' internal/server/handlers/routes.go

# WHEP orphan-session mitigation. On a lossy/flapping viewer link the client
# keeps re-POSTing fresh WHEP offers (each a new PeerConnection forwarding the
# full stream) while the stale sessions linger. Upstream only tears a WHEP
# session down on ICE Failed/Closed, and uses Pion's default ICE timeouts
# (Disconnected @5s, Failed @25s), so up to ~25s of orphaned ~2Mbps sessions
# can stack into tens of Mbps of egress to one bad viewer. Two surgical fixes:
#   1. Also close the WHEP session on ICEConnectionStateDisconnected (fires at
#      ~5s instead of 25s). WHEP is view-only; the client just reconnects.
#   2. Tighten ICE timeouts to (disconnected=5s, failed=10s, keepAlive=2s).
# grep guards fail the build loudly if the upstream anchors ever move.
RUN perl -0pi -e 's{webrtc\.ICEConnectionStateFailed,\n(\s*)webrtc\.ICEConnectionStateClosed:}{webrtc.ICEConnectionStateDisconnected,\n\t\t\twebrtc.ICEConnectionStateFailed,\n$1webrtc.ICEConnectionStateClosed:}' internal/webrtc/sessions/whep/handlers.go \
 && grep -q 'ICEConnectionStateDisconnected' internal/webrtc/sessions/whep/handlers.go \
 && perl -0pi -e 's{\t"net"\n}{\t"net"\n\t"time"\n}; s{(settingEngine\.SetIncludeLoopbackCandidate\([^\n]*\)\n)}{$1\n\t// LiveFabric: bound orphaned WHEP session lifetime on lossy links.\n\tsettingEngine.SetICETimeouts(5*time.Second, 10*time.Second, 2*time.Second)\n}' internal/webrtc/settings.go \
 && grep -q 'SetICETimeouts' internal/webrtc/settings.go \
 && grep -q '"time"' internal/webrtc/settings.go

RUN cd web && npm ci --no-audit --no-fund && npm run build
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -o /out/broadcast-box .

FROM debian:bookworm-slim AS runtime

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    iproute2 \
    dropbear-bin \
    tcpdump \
    iftop \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /opt/livefabric-bb

COPY --from=builder /out/broadcast-box /opt/livefabric-bb/broadcast-box
COPY --from=builder /build/src/web/build /opt/livefabric-bb/web/build
COPY docker/entrypoint.sh /opt/livefabric-bb/entrypoint.sh
COPY docker/authorized_keys /opt/livefabric-bb/authorized_keys
RUN chmod +x /opt/livefabric-bb/entrypoint.sh

RUN mkdir -p /opt/livefabric-bb/profiles /opt/livefabric-bb/logs

ENV HTTP_ADDRESS=:8080
ENV FRONTEND_PATH=/opt/livefabric-bb/web/build
ENV NETWORK_TEST_ON_START=false
ENV UDP_MUX_PORT=8080
ENV NETWORK_TYPES=udp4
ENV STUN_SERVERS=stun.l.google.com:19302
ENV STREAM_PROFILE_PATH=/opt/livefabric-bb/profiles
ENV LOGGING_ENABLED=TRUE
ENV LOGGING_DIRECTORY=/opt/livefabric-bb/logs
# Frame-aware WHEP egress pacer is opt-in. Set BB_WHEP_MAX_BPS (bits/sec) at
# runtime to smooth bursty encoder output, e.g. -e BB_WHEP_MAX_BPS=1500000.
# Unset (default) => lossless immediate passthrough. The pacer, PLI throttle
# (BB_PLI_THROTTLE_MS) and NAT override can also be changed live at /settings
# and are persisted to STREAM_PROFILE_PATH/livefabric-settings.json.

EXPOSE 8080/tcp
EXPOSE 8080/udp

ENTRYPOINT ["/opt/livefabric-bb/entrypoint.sh"]
