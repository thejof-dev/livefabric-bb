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

# Optional on-box SSH for diagnostics (opt-in via SSH_ENABLED). Key-only, root
# login. With --network host this is reachable directly at <host-ip>:<SSH_PORT>,
# and lands you in the host network namespace so tcpdump/iftop/ss see the real
# interface traffic. Host key is persisted under the profiles volume so it stays
# stable across restarts. Extra keys can be added via SSH_AUTHORIZED_KEYS.
case "${SSH_ENABLED:-}" in
  ""|0|false|FALSE|no|NO) ;;
  *)
    SSH_PORT="${SSH_PORT:-22}"
    mkdir -p /root/.ssh && chmod 700 /root/.ssh
    : > /root/.ssh/authorized_keys
    [ -f /opt/livefabric-bb/authorized_keys ] && grep -E '^(ssh-|ecdsa-)' /opt/livefabric-bb/authorized_keys >> /root/.ssh/authorized_keys
    if [ -n "${SSH_AUTHORIZED_KEYS:-}" ]; then
      printf '%s\n' "$SSH_AUTHORIZED_KEYS" >> /root/.ssh/authorized_keys
    fi
    chmod 600 /root/.ssh/authorized_keys

    HOSTKEY_DIR="${STREAM_PROFILE_PATH:-/opt/livefabric-bb/profiles}/dropbear"
    mkdir -p "$HOSTKEY_DIR"
    HOSTKEY="$HOSTKEY_DIR/dropbear_ed25519_host_key"
    [ -f "$HOSTKEY" ] || dropbearkey -t ed25519 -f "$HOSTKEY" >/dev/null 2>&1

    if [ -s /root/.ssh/authorized_keys ]; then
      echo "livefabric-bb: starting dropbear ssh on port ${SSH_PORT} (key-only, root)"
      dropbear -p "${SSH_PORT}" -r "$HOSTKEY" -s -g \
        || echo "livefabric-bb: WARN dropbear failed to start (is port ${SSH_PORT} already in use on the host?)"
    else
      echo "livefabric-bb: WARN SSH_ENABLED set but no authorized keys available; not starting sshd"
    fi
    ;;
esac

exec /opt/livefabric-bb/broadcast-box
