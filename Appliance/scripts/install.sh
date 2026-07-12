#!/usr/bin/env bash
set -euo pipefail
umask 022
[[ ${EUID:-$(id -u)} -eq 0 ]] || { echo 'Run with sudo.' >&2; exit 2; }
ROOT=$(cd "$(dirname "$0")/.." && pwd)
CONFIGURE_NFT=0
for arg in "$@"; do
  case "$arg" in
    --configure-nftables) CONFIGURE_NFT=1 ;;
    --help) echo "Usage: sudo $0 [--configure-nftables]"; exit 0 ;;
    *) echo "Unknown option: $arg" >&2; exit 2 ;;
  esac
done
command -v apt-get >/dev/null || { echo 'Debian/Raspberry Pi OS is required.' >&2; exit 1; }
ARCH=$(dpkg --print-architecture)
[[ $ARCH == arm64 || $ARCH == armhf ]] || { echo "Unsupported architecture: $ARCH" >&2; exit 1; }
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends golang-go cups cups-client cups-filters cups-filters-core-drivers avahi-daemon libnss-mdns ghostscript qpdf poppler-utils file fonts-dejavu-core
cd "$ROOT"
go test ./...
go vet ./...
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /tmp/f11d-install ./cmd/f11d
getent group f11print >/dev/null || groupadd --system f11print
id -u f11print >/dev/null 2>&1 || useradd --system --home-dir /var/lib/f11 --create-home --shell /usr/sbin/nologin --gid f11print f11print
usermod -aG lp f11print
install -d -o root -g root -m0755 /usr/local/lib/f11
install -o root -g root -m0755 /tmp/f11d-install /usr/local/lib/f11/f11d
install -d -o f11print -g lp -m0770 /var/spool/f11
install -d -o f11print -g f11print -m0700 /var/lib/f11
install -o root -g lp -m0660 /dev/null /run/lock/f11-print.lock
install -o root -g root -m0644 "$ROOT/udev/70-rongta-f11.rules" /etc/udev/rules.d/70-rongta-f11.rules
install -o root -g root -m0700 "$ROOT/cups/f11" /usr/lib/cups/backend/f11
install -o root -g root -m0644 "$ROOT/cups/f11.ppd" /usr/share/ppd/f11.ppd
install -o root -g root -m0644 "$ROOT/systemd/f11-health.service" /etc/systemd/system/f11-health.service
printf 'blacklist usblp\n' >/etc/modprobe.d/f11-no-usblp.conf
modprobe -r usblp 2>/dev/null || true
udevadm control --reload-rules
udevadm trigger --subsystem-match=usb --attr-match=idVendor=0fe6 --attr-match=idProduct=811e || true
if [[ $CONFIGURE_NFT -eq 1 ]]; then
  command -v nft >/dev/null || apt-get install -y --no-install-recommends nftables
  [[ -f /etc/nftables.conf ]] || { echo '/etc/nftables.conf does not exist; configure TCP 631 and UDP 5353 manually.' >&2; exit 1; }
  cp -a /etc/nftables.conf "/etc/nftables.conf.f11-backup.$(date +%Y%m%d%H%M%S)"
  python3 "$ROOT/scripts/allow-airprint-nftables.py" /etc/nftables.conf
  nft -c -f /etc/nftables.conf
  nft -f /etc/nftables.conf
fi
systemctl daemon-reload
systemctl enable --now cups avahi-daemon
systemctl enable f11-health.service
lpadmin -x Rongta_F11 2>/dev/null || true
lpadmin -p Rongta_F11 -E -v f11:/ -P /usr/share/ppd/f11.ppd -D 'Rongta F11 Pi AirPrint' -L "$(hostname)" -o printer-is-shared=true
lpadmin -d Rongta_F11
cupsaccept Rongta_F11
cupsenable Rongta_F11
systemctl restart cups avahi-daemon
sudo -u f11print /usr/local/lib/f11/f11d self-test
if sudo -u f11print /usr/local/lib/f11/f11d probe; then
  systemctl start f11-health.service
else
  echo 'F11 is not currently reachable; the enabled boot health check will fail closed until it is connected.' >&2
fi
cat <<EOF
Installed Rongta F11 Pi AirPrint.
Printer URI: ipp://$(hostname).local:631/printers/Rongta_F11
If clients cannot connect, allow LAN TCP 631 and UDP 5353 or rerun with --configure-nftables.
EOF
