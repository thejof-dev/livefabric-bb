# livefabric-bb

**Sub-second WHIP/WHEP WebRTC broadcast server** for LiveFabric, built on
[glimesh/broadcast-box](https://github.com/glimesh/broadcast-box).

Lets any WHIP-capable encoder (OBS ≥30, FFmpeg ≥8, GStreamer, browser) push a
stream and have viewers pull it back over WebRTC with ~120 ms glass-to-glass
latency. Uses a single TCP+UDP port (8080) with NAT 1-to-1 ICE so it works on
flat LANs and behind a single NAT/port-forward.

- **Repo**: https://github.com/glimesh/broadcast-box
- **Protocol in**: WHIP (WebRTC-HTTP Ingestion Protocol)
- **Protocol out**: WHEP (WebRTC-HTTP Egress Protocol)
- **Codecs**: H.264 / VP8 / VP9 / AV1 video, Opus audio
- **Port**: `:8080` TCP (HTTP + signalling) and `:8080` UDP (WebRTC mux)

## Current deployment

| Field | Value |
|---|---|
| Hostname | `livefabric-lora-sdr` (co-tenant on the SDR node) |
| LAN IP | `192.168.98.151` |
| Service | `livefabric-bb.service` |
| Install root | `/opt/livefabric-bb` |
| Binary | `/opt/livefabric-bb/broadcast-box` (Go, statically linked) |
| Frontend | `/opt/livefabric-bb/src/web/build` (Vite/React) |
| Env file | `/opt/livefabric-bb/broadcast-box.env` |
| Profiles dir | `/opt/livefabric-bb/profiles` |
| Logs dir | `/opt/livefabric-bb/logs` |
| Admin token | `livefabric-admin` |

## URLs

- GUI / player: `http://192.168.98.151:8080/`
- Browser publisher: `http://192.168.98.151:8080/publish/<streamKey>`
- Player for one stream: `http://192.168.98.151:8080/<streamKey>`
- Statistics: `http://192.168.98.151:8080/statistics`
- Admin portal: `http://192.168.98.151:8080/admin` (token: `livefabric-admin`)
- WHIP ingest endpoint: `http://192.168.98.151:8080/api/whip`
- WHEP egress endpoint: `http://192.168.98.151:8080/api/whep`
- Status JSON: `http://192.168.98.151:8080/api/status`

## OBS settings

- **Service**: WHIP
- **Server**: `http://192.168.98.151:8080/api/whip`
- **Stream Key**: any string (e.g. `racetest`); same key is used to view at
  `http://192.168.98.151:8080/racetest`
- **Encoder**: x264, tune `zerolatency`, keyframe interval 2 s for sub-second

## FFmpeg test publish

```bash
ffmpeg -re \
  -f lavfi -i testsrc=size=1280x720:rate=30 \
  -f lavfi -i sine=frequency=440 \
  -pix_fmt yuv420p -vcodec libx264 -profile:v baseline -r 30 -g 60 \
  -acodec libopus -ar 48000 -ac 2 \
  -f whip -authorization "ffmpegtest" \
  "http://192.168.98.151:8080/api/whip"
```

Then view at `http://192.168.98.151:8080/ffmpegtest`.

## Install (manual, what was actually run)

```bash
# 1. toolchain
sudo apt-get update
sudo apt-get install -y golang-go nodejs npm build-essential git ca-certificates

# 2. fetch source
sudo mkdir -p /opt/livefabric-bb && sudo chown $USER:$USER /opt/livefabric-bb
cd /opt/livefabric-bb
git clone --depth 1 https://github.com/glimesh/broadcast-box.git src

# 3. build frontend
cd /opt/livefabric-bb/src/web
npm install --no-audit --no-fund
npm run build

# 4. build backend
cd /opt/livefabric-bb/src
go build -o /opt/livefabric-bb/broadcast-box .

# 5. env + dirs
mkdir -p /opt/livefabric-bb/profiles /opt/livefabric-bb/logs
cp install/broadcast-box.env /opt/livefabric-bb/broadcast-box.env

# 6. systemd
sudo cp systemd/livefabric-bb.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now livefabric-bb.service

# 7. firewall (ufw)
sudo ufw allow 8080/tcp comment broadcast-box-http
sudo ufw allow 8080/udp comment broadcast-box-webrtc
```

## Updating

```bash
cd /opt/livefabric-bb/src
git pull
( cd web && npm install --no-audit --no-fund && npm run build )
go build -o /opt/livefabric-bb/broadcast-box .
sudo systemctl restart livefabric-bb.service
```

## Docker (GHCR) for encoders

This repo now builds and publishes a hardened Broadcast Box image to GitHub
Container Registry (GHCR) using the workflow in `.github/workflows/livefabric-bb-docker.yml`.

Published image:

```bash
ghcr.io/<your-org-or-user>/livefabric-bb:latest
```

Pull on an encoder:

```bash
docker pull ghcr.io/<your-org-or-user>/livefabric-bb:latest
```

Current encoder workflow (UI steps):

- Click on `+ Pull Image`
- Click on `+ Create Container`
- When created, click on `Manage` for the BB container
- If needed, create a new video profile with the resolution and bitrate for your application
- If audio is needed, create an Opus audio output profile
- Create a WHIP output using the video and audio (if needed) profiles
- WHIP URL: `http://127.0.0.1:8080/api/whip`
- Authorization token: arbitrary string (for example `SailGP2171Videon`)
- Enable the stream and click save
- Playback link: `http://{device_ip}:8080/{Authorization_Token}`

Run on an encoder (host networking recommended for WebRTC):

```bash
docker run -d --name livefabric-bb --restart unless-stopped \
  --network host \
  -e NAT_1_TO_1_IP=192.168.60.89 \
  -e INTERFACE_FILTER=enx98fc84e62829 \
  -e FRONTEND_ADMIN_TOKEN=livefabric-admin \
  -v /opt/livefabric-bb/profiles:/opt/livefabric-bb/profiles \
  -v /opt/livefabric-bb/logs:/opt/livefabric-bb/logs \
  ghcr.io/<your-org-or-user>/livefabric-bb:latest
```

If host networking is not allowed, publish both TCP+UDP 8080 explicitly:

```bash
docker run -d --name livefabric-bb --restart unless-stopped \
  -p 8080:8080/tcp -p 8080:8080/udp \
  -e NAT_1_TO_1_IP=<encoder-lan-ip> \
  -e INTERFACE_FILTER=<encoder-nic-name> \
  ghcr.io/<your-org-or-user>/livefabric-bb:latest
```

Notes:

- `NAT_1_TO_1_IP` must be the IP viewers can actually reach.
- `INTERFACE_FILTER` should match the LAN NIC on that encoder host.
- The Docker image includes the same mitigation we applied live:
  reduced RTCP feedback and throttled PLI forwarding.

### Burst control on BB server

To reduce traffic spikes caused by bursty encoder output (large keyframes, VBR
overshoot), this image includes a **frame-aware egress pacer** in the WHEP
packet path. It is a leaky-bucket pacer with a bounded per-session queue: it
*smooths* bursts by delaying packets to a steady target rate rather than
dropping them, so it is lossless for transient spikes and only adds a small,
bounded latency. It never slices individual packets out of a frame — under
sustained overload it flushes the pending queue and cleanly resyncs on the next
keyframe (via a throttled PLI), so a decoder is never fed a partial frame.

- Env var: `BB_WHEP_MAX_BPS`
- Meaning: target pacing rate (bits/sec) for per-session WHEP video egress
- Default: **unset = disabled** (lossless immediate passthrough). Opt-in only.

This complements — it does not replace — encoder-side rate control. The most
effective burst fix remains capping the encoder itself (CBR/constant-strict +
longer GOP); the pacer smooths whatever the encoder still bursts.

Example (enable pacing at 1.5 Mbps):

```bash
docker run -d --name livefabric-bb --restart unless-stopped \
  --network host \
  -e NAT_1_TO_1_IP=<encoder-lan-ip> \
  -e INTERFACE_FILTER=<encoder-nic-name> \
  -e BB_WHEP_MAX_BPS=1500000 \
  ghcr.io/<your-org-or-user>/livefabric-bb:latest
```

Set the rate at or slightly above the encoder's average bitrate. If set well
below the sustained bitrate the queue will keep overflowing (repeated PLI /
brief freezes); that is backpressure telling you the encoder rate is too high.

### Client/player-side hardening

Server-side pacing helps, but unstable clients still need guardrails:

- keep one active playback session per device/tab
- avoid auto-reconnect loops with sub-second retry; use exponential backoff
- on packet-loss events, debounce quality/layer switches to at most once every 2-3s
- prefer fixed lower layer/profile for poor links instead of rapid adaptive toggling
- if browser autoplay fails, do not hammer play calls in a tight loop

## Troubleshooting

- **404 on `/`** — `DISABLE_FRONTEND` is set to *anything* (incl. `FALSE`); the
  Go server treats any non-empty value as disabled. Remove the line entirely
  from `broadcast-box.env`.
- **Connects to GUI, OBS push fails / video freezes** — UDP/8080 blocked, or
  `NAT_1_TO_1_IP` doesn't include the IP the client sees. Add every interface
  IP the clients route to, pipe-separated (e.g. `192.168.98.151|10.0.0.5`).
- **Connect from another LAN times out** — `ufw` has no rule; run the two
  `ufw allow 8080/...` commands above.
- **High CPU / dropped frames** — broadcast-box just forwards SRTP; if it
  pegs a core, the encoder is sending too high a bitrate or you're running
  the GUI build proxy in parallel — kill `npm run start`/`vite dev`.

See [docs/LIVEFABRIC-BB-NODE.md](../docs/LIVEFABRIC-BB-NODE.md) for the
node-type spec.
