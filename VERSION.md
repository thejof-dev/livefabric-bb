# LiveFabric-BB — Version & Build State

Fork of [broadcast-box](https://github.com/Glimesh/broadcast-box) (Go WHIP/WHEP
WebRTC SFU) customized for LiveFabric edge nodes. Image published to
`ghcr.io/thejof-dev/livefabric-bb`.

## Current deployment

| Channel | Tag | Commit | State |
|---|---|---|---|
| **Production** | `:latest` | `5f8a365` | **Safe no-NACK build.** Deployed on all nodes. |
| Trial | `:v13` | `57003d5` | FlexFEC-03 forward repair (no NACK). Opt-in, per-node trial. |
| Known-bad | `:v12` | `5d792f7` | Bounded NACK responder — **still storms, do not deploy.** |
| Pre-NACK good | `:sha-c9cf07e` | `c9cf07e` | Last known-good before the NACK experiments. |

All links run over **SpeedFusion** on uncontrolled 5G/WAN. Containers run
`--network host`; UDP mux on `:8080`. Encoder GOP = 1 s.

## Root cause of the egress-spike / freeze incident

The production egress spikes were **not** a simple NACK buffer-size problem. The
trigger is **5G SA (standalone) UDP packet reordering**:

1. 5G SA reorders UDP packets on the uplink.
2. SpeedFusion holds packets in its reorder buffer trying to restore sequence,
   adding latency and presenting apparent gaps to the WHEP receiver.
3. The receiver interprets the gaps as loss and sends RTCP **NACKs**.
4. broadcast-box has **no congestion control**, so it retransmits faster; the
   retransmits pile back into the SpeedFusion buffer — a positive-feedback
   **flood** that saturates egress.

This is why bounding `nack.ResponderSize` (v12) had no effect: the storm is
driven by reordering-induced *false* loss plus the SF buffer, independent of the
responder ring size. **Any NACK on the WHEP egress path amplifies it.**

## Fix strategy

- **`:latest` removes NACK entirely** (see `docker/overrides/internal/webrtc/interceptors/interceptors.go`).
  PLI/FIR → keyframe recovery remains as the large-loss fallback (~1 s GOP cap on
  freeze duration). This kills the amplifier.
- **`:v13` adds FlexFEC-03** — fixed-overhead forward repair with **no feedback
  loop**, so it can repair reorder/loss without racing the SpeedFusion buffer.
  Payload type `49`, default 5 media / 2 FEC (~40 % overhead, trivial at ~150 kbps).
  The FlexFEC generator is a **no-op unless the viewer negotiates the FlexFEC-03
  codec**, so a browser without support behaves exactly like `:latest`.

Additional levers to attack the trigger itself (network side, not code):
- Reduce 5G SA out-of-order delivery (modem/RLC reordering settings).
- Tune down / disable the SpeedFusion reorder/receive buffer and SF FEC.
- Increase the receiver jitter/playout buffer to tolerate reorder before
  declaring loss.

## Build & release

CI: `.github/workflows/livefabric-bb-docker.yml`. Triggers on push to `main`
(paths `docker/**` + the workflow file), on tags `v*`, and `workflow_dispatch`.
Builds `linux/amd64,linux/arm64`.

- Push to `main` → rebuilds `:latest` + `:sha-<short>`.
- Push a tag `vN` → builds `:vN` (does **not** move `:latest`).

Docker overlay applies override files from `docker/overrides/` plus perl patches
in `docker/Dockerfile` (routes, ICE disconnect handling, ICE timeouts, closed-pipe
propagation).

## Trialing v13

Deploy on one node (with Peplink SpeedFusion FEC removed to avoid double-FEC):

```bash
docker pull ghcr.io/thejof-dev/livefabric-bb:v13
docker rm -f livefabric-bb 2>/dev/null
docker run -d --name livefabric-bb --network host --restart unless-stopped \
  ghcr.io/thejof-dev/livefabric-bb:v13
```

Then confirm the WHEP answer SDP advertises `a=rtpmap:49 flexfec-03` **and** that
the browser actually decodes/repairs. If the browser ignores FlexFEC it is a
no-op → fall back to GOP tuning as the reliable mitigation. If it works → promote
to `:latest`.
