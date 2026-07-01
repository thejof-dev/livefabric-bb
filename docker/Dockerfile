# syntax=docker/dockerfile:1.7

FROM node:22-bookworm-slim AS node

FROM golang:1.24-bookworm AS builder

ARG BROADCAST_BOX_REF=main
WORKDIR /build

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

# Mitigation for bad-link viewer feedback loops:
# 1) reduce advertised RTCP feedback (drop nack/fir/pli)
# 2) rate-limit PLI forwarding to publisher side
RUN perl -0777 -i -pe 's/var videoRTCPFeedback = \[\]webrtc\.RTCPFeedback\{\n\s*\{Type: "goog-remb", Parameter: ""\},\n\s*\{Type: "ccm", Parameter: "fir"\},\n\s*\{Type: "nack", Parameter: ""\},\n\s*\{Type: "nack", Parameter: "pli"\},\n\}/var videoRTCPFeedback = []webrtc.RTCPFeedback{\n\t\t{Type: "goog-remb", Parameter: ""},\n\t}/s' internal/webrtc/codecs/codecs.go

RUN perl -0777 -i -pe 's/(SessionClose sync\.Once\n\s*onClose\s+func\(string\)\n\s*pliSender\s+func\(\)\n)/$1\n\t\t\t\tPLILock     sync.Mutex\n\t\t\t\tlastPLISent time.Time\n/s' internal/webrtc/sessions/whep/types.go

RUN perl -0777 -i -pe 's/func \(w \*WHEPSession\) SendPLI\(\) \{\n\s*if w\.IsSessionClosed\.Load\(\) \{\n\s*return\n\s*\}\n\n\s*w\.pliSender\(\)\n\}/func (w *WHEPSession) SendPLI() {\n\tif w.IsSessionClosed.Load() {\n\t\treturn\n\t}\n\n\tconst minPLIInterval = 750 * time.Millisecond\n\tnow := time.Now()\n\n\tw.PLILock.Lock()\n\tif !w.lastPLISent.IsZero() && now.Sub(w.lastPLISent) < minPLIInterval {\n\t\tw.PLILock.Unlock()\n\t\treturn\n\t}\n\tw.lastPLISent = now\n\tw.PLILock.Unlock()\n\n\tw.pliSender()\n\}/s' internal/webrtc/sessions/whep/whep.go

RUN cd web && npm ci --no-audit --no-fund && npm run build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/broadcast-box .

FROM debian:bookworm-slim AS runtime

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /opt/livefabric-bb

COPY --from=builder /out/broadcast-box /opt/livefabric-bb/broadcast-box
COPY --from=builder /build/src/web/build /opt/livefabric-bb/web/build

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

EXPOSE 8080/tcp
EXPOSE 8080/udp

ENTRYPOINT ["/opt/livefabric-bb/broadcast-box"]
