#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C
umask 077
[[ ${EUID:-$(id -u)} -eq 0 ]] || { echo 'Run with sudo.' >&2; exit 2; }
ROOT=$(cd "$(dirname "$0")/.." && pwd)
CLIENT_ID=${TWITCH_CLIENT_ID:-}
CHANNEL=${TWITCH_CHANNEL:-}
BROADCASTER_ID=${TWITCH_BROADCASTER_ID:-}
QUEUE=${F11_QUEUE:-Rongta_F11_Media}
[[ $CLIENT_ID =~ ^[a-z0-9]{20,64}$ ]] || { echo 'Set a valid TWITCH_CLIENT_ID.' >&2; exit 2; }
[[ $CHANNEL =~ ^[a-zA-Z0-9_]{1,25}$ ]] || { echo 'Set a valid TWITCH_CHANNEL.' >&2; exit 2; }
[[ $BROADCASTER_ID =~ ^[0-9]{1,20}$ ]] || { echo 'Set a valid TWITCH_BROADCASTER_ID.' >&2; exit 2; }
[[ $QUEUE =~ ^Rongta_F11[A-Za-z0-9_.-]{0,115}$ ]] || { echo 'Set a valid F11_QUEUE.' >&2; exit 2; }
command -v bannerprint >/dev/null || { echo 'bannerprint is not installed.' >&2; exit 1; }
test -x /usr/local/lib/f11/check-f11-runtime || { echo 'F11 runtime verifier is not installed.' >&2; exit 1; }
QUEUE_LINE=$(lpstat -v "$QUEUE" 2>/dev/null) || { echo 'Configured F11 queue does not exist.' >&2; exit 1; }
/usr/local/lib/f11/check-f11-runtime "$QUEUE" "$QUEUE_LINE" /sys/bus/usb/devices >/dev/null || { echo 'Configured queue is not the attached F11.' >&2; exit 1; }
read -r -s -p 'Twitch Client Secret: ' CLIENT_SECRET </dev/tty
printf '\n' >/dev/tty
[[ $CLIENT_SECRET =~ ^[a-z0-9]{20,128}$ ]] || { unset CLIENT_SECRET; echo 'Invalid Twitch Client Secret.' >&2; exit 2; }
BUILD=$(mktemp -d /root/twitch-banner-install.XXXXXX)
trap 'rm -rf "$BUILD"; unset CLIENT_SECRET' EXIT
cd "$ROOT"
go test ./internal/twitchbanner ./cmd/twitch-banner
go vet ./internal/twitchbanner ./cmd/twitch-banner
MACHINE=$(uname -m)
if [[ $MACHINE == armv6l ]]; then
  CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 go build -trimpath -ldflags='-s -w' -o "$BUILD/twitch-banner" ./cmd/twitch-banner
else
  CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o "$BUILD/twitch-banner" ./cmd/twitch-banner
fi
file "$BUILD/twitch-banner" | grep -Eq 'ARM|aarch64|x86-64' || { echo 'Unexpected twitch-banner architecture.' >&2; exit 1; }
id -u twitch-banner >/dev/null 2>&1 || useradd --system --home-dir /var/lib/twitch-banner --create-home --shell /usr/sbin/nologin twitch-banner
usermod -aG lp twitch-banner
install -d -o root -g twitch-banner -m0750 /etc/twitch-banner
install -d -o twitch-banner -g twitch-banner -m0700 /var/lib/twitch-banner
install -o root -g root -m0755 "$BUILD/twitch-banner" /usr/local/bin/twitch-banner
ENV_NEW=$(mktemp /etc/twitch-banner/environment.new.XXXXXX)
{
  printf 'TWITCH_CLIENT_ID=%s\n' "$CLIENT_ID"
  printf 'TWITCH_CLIENT_SECRET=%s\n' "$CLIENT_SECRET"
  printf 'TWITCH_CHANNEL=%s\n' "${CHANNEL,,}"
  printf 'TWITCH_BROADCASTER_ID=%s\n' "$BROADCASTER_ID"
  printf 'F11_QUEUE=%s\n' "$QUEUE"
  printf 'TWITCH_TOKEN_FILE=/var/lib/twitch-banner/token.json\n'
  printf 'TWITCH_JOURNAL_FILE=/var/lib/twitch-banner/events.jsonl\n'
} >"$ENV_NEW"
chown root:twitch-banner "$ENV_NEW"
chmod 0640 "$ENV_NEW"
mv -f "$ENV_NEW" /etc/twitch-banner/environment
install -o root -g root -m0644 "$ROOT/systemd/twitch-banner.service" /etc/systemd/system/twitch-banner.service
install -d -o root -g root -m0755 /usr/local/sbin
install -o root -g root -m0755 "$ROOT/scripts/twitch-banner-authorize" /usr/local/sbin/twitch-banner-authorize
systemctl daemon-reload
systemctl enable twitch-banner.service
systemctl stop twitch-banner.service 2>/dev/null || true
printf '%s\n' 'Installed twitch-banner. Authorize before starting with the protected environment file.'
