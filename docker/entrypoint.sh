#!/bin/sh
# LiveFabric BB entrypoint.
#
# Makes the appliance "just work" on host networking without knowing the NIC
# name: if NAT_1_TO_1_IP is not provided, use the settings-file override if set,
# otherwise auto-detect the primary LAN IPv4 (the source IP for outbound
# traffic). This yields a single, correct ICE host candidate for LAN viewers.
set -eu

if [ -n "${BB_SETTINGS_PATH:-}" ]; then
  SETTINGS_FILE="$BB_SETTINGS_PATH"
else
  SETTINGS_FILE="${STREAM_PROFILE_PATH:-/opt/livefabric-bb/profiles}/livefabric-settings.json"
fi

if [ -z "${NAT_1_TO_1_IP:-}" ]; then
  OVERRIDE=""
  if [ -f "$SETTINGS_FILE" ]; then
    OVERRIDE=$(sed -n 's/.*"natOverrideIp"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$SETTINGS_FILE" | head -n1)
  fi

  if [ -n "$OVERRIDE" ]; then
    NAT_1_TO_1_IP="$OVERRIDE"
  else
    DETECTED=$(ip route get 1.1.1.1 2>/dev/null | sed -n 's/.*src \([0-9.]*\).*/\1/p' | head -n1)
    if [ -z "$DETECTED" ]; then
      DETECTED=$(hostname -I 2>/dev/null | awk '{print $1}')
    fi
    if [ -n "$DETECTED" ]; then
      NAT_1_TO_1_IP="$DETECTED"
    fi
  fi

  if [ -n "${NAT_1_TO_1_IP:-}" ]; then
    export NAT_1_TO_1_IP
  fi
fi

echo "livefabric-bb: NAT_1_TO_1_IP=${NAT_1_TO_1_IP:-<default candidate gathering>}"
exec /opt/livefabric-bb/broadcast-box
