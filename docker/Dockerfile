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

RUN cd web && npm ci --no-audit --no-fund && npm run build
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -o /out/broadcast-box .

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
ENV BB_WHEP_MAX_BPS=1500000

EXPOSE 8080/tcp
EXPOSE 8080/udp

ENTRYPOINT ["/opt/livefabric-bb/broadcast-box"]
