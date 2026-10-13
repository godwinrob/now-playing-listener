#!/bin/bash
# Installs the Now Playing listener on a Raspberry Pi (Raspberry Pi OS Lite or Desktop): a systemd
# service that records from an audio input, such as a USB sound card's line-in next to the turntable,
# and reports what it hears to the Now Playing server. Safe to run again: it only changes what
# differs, and says so. Upgrading is the same: unpack the newer release and run it again.
#
#   tar xzf now-playing-listener_v1.2.3_linux_arm64.tar.gz && cd now-playing-listener_v1.2.3_linux_arm64
#   sudo ./setup.sh --server http://tower.local:8080 --api-key npk_... --audio-device plughw:1,0
#
# --api-key        made on the server's Host page ("API keys"); --token is the same thing.
# --audio-device   the ALSA capture device: run `arecord -l`; "card 1: ... device 0" is plughw:1,0.
# --binary         a listener binary to install. Not needed from a release tarball, which has one
#                  next to this script.
# Without --server and --api-key the first run writes /etc/now-playing/listener.env and stops so you
# can fill it in.
set -euo pipefail

SERVER="" KEY="" DEVICE="" BINARY=""
while [ $# -gt 0 ]; do
  case "$1" in
    --server) SERVER="$2"; shift 2 ;;
    --api-key|--token) KEY="$2"; shift 2 ;;
    --audio-device) DEVICE="$2"; shift 2 ;;
    --binary) BINARY="$2"; shift 2 ;;
    -h|--help) sed -n '2,17p' "$0"; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 2 ;;
  esac
done

[ "$(id -u)" -eq 0 ] || { echo "Run with sudo." >&2; exit 1; }

HERE=$(cd "$(dirname "$0")" && pwd)
ENV_FILE=/etc/now-playing/listener.env
RELEASES=https://github.com/godwinrob/now-playing-listener/releases
CHANGED=0
note() { echo "    $*"; CHANGED=1; }

# put SRC DEST MODE: installs SRC at DEST only when the content or mode differs.
put() {
  if [ -f "$2" ] && cmp -s "$1" "$2" && [ "$(stat -c %a "$2")" = "${3#0}" ]; then
    return 0
  fi
  install -D -m "$3" "$1" "$2"
  note "updated $2"
}

# set_var NAME VALUE: sets NAME=VALUE in the env file without treating the value as a pattern.
set_var() {
  if grep -qxF "$1=$2" "$ENV_FILE"; then return 0; fi
  { grep -v "^$1=" "$ENV_FILE" || true; printf '%s=%s\n' "$1" "$2"; } > "$ENV_FILE.tmp"
  cat "$ENV_FILE.tmp" > "$ENV_FILE"
  rm -f "$ENV_FILE.tmp"
  note "set $1 in $ENV_FILE"
}

echo "==> Packages"
MISSING=""
for p in alsa-utils curl; do dpkg -s "$p" >/dev/null 2>&1 || MISSING="$MISSING $p"; done
if [ -n "$MISSING" ]; then
  apt-get update -qq
  # shellcheck disable=SC2086 # the list is meant to split into words
  apt-get install -y -qq $MISSING
  note "installed$MISSING"
fi

echo "==> Listener binary"
# A release tarball has the binary next to this script.
if [ -z "$BINARY" ] && [ -f "$HERE/now-playing-listener" ]; then BINARY="$HERE/now-playing-listener"; fi
TMP_BIN=$(mktemp)
trap 'rm -f "$TMP_BIN"' EXIT
if [ -n "$BINARY" ]; then
  cp "$BINARY" "$TMP_BIN"
else
  case "$(uname -m)" in
    aarch64|arm64) ARCH=arm64 ;;
    armv6l|armv7l) ARCH=armv6 ;;
    *) echo "No listener build for this CPU ($(uname -m))." >&2; exit 1 ;;
  esac
  # Works while the repository is public. Otherwise download a release tarball (see the README).
  if ! curl -fsSL -o "$TMP_BIN" "$RELEASES/latest/download/now-playing-listener-linux-$ARCH"; then
    echo "Could not download the listener from $RELEASES (a private repository needs a login)." >&2
    echo "Download a release tarball instead and run this script from inside it; see the README." >&2
    exit 1
  fi
fi
chmod 755 "$TMP_BIN"
if ! VERSION=$("$TMP_BIN" version 2>/dev/null); then
  echo "This listener build doesn't run on this Pi ($(uname -m))." >&2
  echo "Use the arm64 tarball on 64-bit Raspberry Pi OS and the armv6 one on 32-bit." >&2
  exit 1
fi
put "$TMP_BIN" /usr/local/bin/now-playing-listener 0755

echo "==> Service user"
if ! id nowplaying >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /usr/sbin/nologin nowplaying
  note "created user nowplaying"
fi
if ! id -nG nowplaying | grep -qw audio; then
  usermod -aG audio nowplaying
  note "added nowplaying to the audio group"
fi

echo "==> Settings in $ENV_FILE"
if [ ! -f "$ENV_FILE" ]; then
  install -D -m 0600 -o root -g root "$HERE/listener.env.example" "$ENV_FILE"
  note "created $ENV_FILE"
fi
[ -z "$SERVER" ] || set_var NPL_SERVER_URL "$SERVER"
if [ -n "$KEY" ]; then
  set_var NPL_API_KEY "$KEY"
  # The key used to be called NPL_DEVICE_TOKEN; drop an old line so there is one source of truth.
  if grep -q '^NPL_DEVICE_TOKEN=' "$ENV_FILE"; then
    sed -i '/^NPL_DEVICE_TOKEN=/d' "$ENV_FILE"
    note "removed the old NPL_DEVICE_TOKEN from $ENV_FILE"
  fi
fi
[ -z "$DEVICE" ] || set_var NPL_ALSA_DEVICE "$DEVICE"
# The file holds the API key: root only. systemd reads it as root before starting the service.
chown root:root "$ENV_FILE"
chmod 0600 "$ENV_FILE"
if grep -q '^NPL_API_KEY=npk_replace-me$' "$ENV_FILE" || grep -q '^NPL_SERVER_URL=http://tower.local:8080$' "$ENV_FILE"; then
  echo
  echo "Fill in NPL_SERVER_URL and NPL_API_KEY in $ENV_FILE (sudo nano $ENV_FILE),"
  echo "or rerun with --server and --api-key, then run this script again."
  exit 1
fi

echo "==> systemd service"
put "$HERE/now-playing-listener.service" /etc/systemd/system/now-playing-listener.service 0644
if [ "$CHANGED" -eq 1 ]; then
  systemctl daemon-reload
  systemctl enable --quiet now-playing-listener.service
  systemctl restart now-playing-listener.service
fi
if ! systemctl is-enabled --quiet now-playing-listener.service; then
  systemctl enable --now --quiet now-playing-listener.service
  note "enabled now-playing-listener"
fi

echo
if [ "$CHANGED" -eq 0 ]; then
  echo "Nothing to change: this Pi is already set up ($VERSION)."
else
  echo "Done. The listener is running: $VERSION."
fi
if grep -q '^NPL_ALSA_DEVICE=default$' "$ENV_FILE"; then
  echo
  echo "Audio input is still 'default'. Capture devices on this Pi:"
  arecord -l || true
  echo "Pick yours (card N, device M is plughw:N,M) and rerun with --audio-device plughw:N,M"
fi
echo
echo "Check the input level:  sudo systemctl stop now-playing-listener && sudo sh -c 'set -a; . $ENV_FILE; now-playing-listener test-mic'; sudo systemctl start now-playing-listener"
echo "Service status:         systemctl status now-playing-listener"
echo "Live log:               journalctl -u now-playing-listener -f"
