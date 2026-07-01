#!/usr/bin/env bash
# LiveFabric Broadcast Box installer — Ubuntu 24.04+
set -euo pipefail

ROOT=/opt/livefabric-bb
REPO=https://github.com/glimesh/broadcast-box.git
HERE="$(cd "$(dirname "$0")" && pwd)"

sudo apt-get update
sudo apt-get install -y golang-go nodejs npm build-essential git ca-certificates ufw

sudo mkdir -p "$ROOT"
sudo chown "$USER":"$USER" "$ROOT"

if [ ! -d "$ROOT/src/.git" ]; then
  git clone --depth 1 "$REPO" "$ROOT/src"
else
  ( cd "$ROOT/src" && git pull )
fi

( cd "$ROOT/src/web" && npm install --no-audit --no-fund && npm run build )
( cd "$ROOT/src"     && go build -o "$ROOT/broadcast-box" . )

mkdir -p "$ROOT/profiles" "$ROOT/logs"
[ -f "$ROOT/broadcast-box.env" ] || cp "$HERE/broadcast-box.env" "$ROOT/broadcast-box.env"

sudo cp "$HERE/../systemd/livefabric-bb.service" /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now livefabric-bb.service

sudo ufw allow 8080/tcp comment broadcast-box-http  || true
sudo ufw allow 8080/udp comment broadcast-box-webrtc || true

systemctl is-active livefabric-bb.service
echo "GUI:    http://$(hostname -I | awk '{print $1}'):8080/"
echo "Admin:  http://$(hostname -I | awk '{print $1}'):8080/admin (token in env file)"
