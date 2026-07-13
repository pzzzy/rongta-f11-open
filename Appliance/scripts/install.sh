#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C
umask 022
[[ ${EUID:-$(id -u)} -eq 0 ]] || { echo 'Run with sudo.' >&2; exit 2; }
ROOT=$(cd "$(dirname "$0")/.." && pwd)
BUILD_DIR=$(mktemp -d /tmp/f11-install.XXXXXX)
NFT_TMP=
MIGRATION_MARKER=/var/lib/f11/migration-in-progress
MIGRATION_ACTIVE=0
cleanup() {
  if [[ $MIGRATION_ACTIVE -eq 1 ]]; then
    cupsdisable Rongta_F11 >/dev/null 2>&1 || true
    cupsreject Rongta_F11 >/dev/null 2>&1 || true
    install -o root -g root -m0755 "$ROOT/cups/f11-migration-hold" /usr/lib/cups/backend/f11 2>/dev/null || true
    echo 'F11 migration stopped safely with the queue disabled. Fix the reported error and rerun the installer.' >&2
  fi
  rm -rf "$BUILD_DIR"
  [[ -z $NFT_TMP ]] || rm -f "$NFT_TMP"
}
trap cleanup EXIT
CONFIGURE_NFT=0
UNMASK_CUPS=0
for arg in "$@"; do
  case "$arg" in
    --configure-nftables) CONFIGURE_NFT=1 ;;
    --unmask-cups) UNMASK_CUPS=1 ;;
    --help) echo "Usage: sudo $0 [--configure-nftables] [--unmask-cups]"; exit 0 ;;
    *) echo "Unknown option: $arg" >&2; exit 2 ;;
  esac
done
if systemctl is-enabled cups.service 2>/dev/null | grep -Fqx masked; then
  [[ $UNMASK_CUPS -eq 1 ]] || { echo 'CUPS is administratively masked; rerun with --unmask-cups to explicitly enable it.' >&2; exit 1; }
  systemctl unmask cups.service cups.socket cups.path
  systemctl unmask --runtime cups.service cups.socket cups.path
  for unit in cups.service cups.socket cups.path; do
    [[ $(systemctl is-enabled "$unit" 2>/dev/null || true) != masked ]] || { echo "Unable to unmask $unit; remove its systemd mask explicitly." >&2; exit 1; }
  done
fi
command -v apt-get >/dev/null || { echo 'Debian/Raspberry Pi OS is required.' >&2; exit 1; }
ARCH=$(dpkg --print-architecture)
[[ $ARCH == arm64 || $ARCH == armhf ]] || { echo "Unsupported architecture: $ARCH" >&2; exit 1; }
# Quarantine an existing legacy f11:/ queue before package installation can
# start or restart CUPS. This backend never reads job data or opens USB.
if [[ -x /usr/lib/cups/backend/f11 || -f /etc/cups/printers.conf ]]; then
  install -d -o root -g root -m0755 /usr/lib/cups/backend
  install -o root -g root -m0755 "$ROOT/cups/f11-migration-hold" /usr/lib/cups/backend/f11
fi
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends golang-go cups cups-client cups-filters cups-filters-core-drivers avahi-daemon libnss-mdns ghostscript qpdf poppler-utils file fonts-dejavu-core
cd "$ROOT"
go test ./...
go vet ./...
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o "$BUILD_DIR/f11d" ./cmd/f11d
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o "$BUILD_DIR/bannerprint" ./cmd/bannerprint
getent group f11print >/dev/null || groupadd --system f11print
id -u f11print >/dev/null 2>&1 || useradd --system --home-dir /var/lib/f11 --create-home --shell /usr/sbin/nologin --gid f11print f11print
usermod -aG lp f11print
install -d -o root -g root -m0755 /usr/local/lib/f11
install -o root -g root -m0755 "$BUILD_DIR/f11d" /usr/local/lib/f11/f11d
install -d -o root -g root -m0755 /usr/local/bin
install -o root -g root -m0755 "$BUILD_DIR/bannerprint" /usr/local/bin/bannerprint
install -d -o root -g root -m0755 /usr/share/doc/rongta-f11-open
install -o root -g root -m0644 "$ROOT/internal/banner/assets/ComicNeue-OFL.txt" /usr/share/doc/rongta-f11-open/ComicNeue-OFL.txt
install -o root -g root -m0755 "$ROOT/scripts/f11-health.sh" /usr/local/lib/f11/f11-health
install -o root -g root -m0755 "$ROOT/scripts/plan-queue-migration.py" /usr/local/lib/f11/plan-queue-migration
install -o root -g root -m0755 "$ROOT/scripts/check-f11-runtime.py" /usr/local/lib/f11/check-f11-runtime
install -o root -g root -m0755 "$ROOT/scripts/pdf-page-height.py" /usr/local/lib/f11/pdf-page-height
install -o root -g root -m0755 "$ROOT/scripts/media-canvas.py" /usr/local/lib/f11/media-canvas
install -d -o lp -g lp -m0700 /var/spool/f11
install -d -o f11print -g f11print -m0700 /var/lib/f11
install -o root -g root -m0755 "$ROOT/cups/pdftof11" /usr/lib/cups/filter/pdftof11
install -o root -g root -m0755 "$ROOT/cups/f11-migration-hold" /usr/lib/cups/backend/f11
install -o root -g root -m0644 "$ROOT/cups/f11.ppd" /usr/share/ppd/f11.ppd
install -o root -g root -m0644 "$ROOT/cups/0fe6-811e.usb-quirks" /usr/share/cups/usb/0fe6-811e.usb-quirks
install -o root -g root -m0644 "$ROOT/systemd/f11-health.service" /etc/systemd/system/f11-health.service
rm -f /etc/modprobe.d/f11-no-usblp.conf
modprobe usblp || echo 'Warning: usblp is unavailable; CUPS libusb transport can still operate.' >&2
if [[ $CONFIGURE_NFT -eq 1 ]]; then
  command -v nft >/dev/null || apt-get install -y --no-install-recommends nftables
  [[ -f /etc/nftables.conf ]] || { echo '/etc/nftables.conf does not exist; configure TCP 631 and UDP 5353 manually.' >&2; exit 1; }
  NFT_TMP=$(mktemp /etc/nftables.conf.f11-new.XXXXXX)
  python3 "$ROOT/scripts/allow-airprint-nftables.py" /etc/nftables.conf "$NFT_TMP"
  chmod --reference=/etc/nftables.conf "$NFT_TMP"
  chown --reference=/etc/nftables.conf "$NFT_TMP"
  nft -c -f "$NFT_TMP"
  cp -a /etc/nftables.conf "/etc/nftables.conf.f11-backup.$(date +%Y%m%d%H%M%S)"
  mv -f "$NFT_TMP" /etc/nftables.conf
  nft -f /etc/nftables.conf
fi
systemctl daemon-reload
systemctl enable --now cups avahi-daemon
systemctl enable f11-health.service
CURRENT_QUEUE=$(lpstat -v Rongta_F11 2>/dev/null || true)
F11_USB_URI=$(/usr/lib/cups/backend/usb 2>/dev/null | /usr/local/lib/f11/plan-queue-migration "$CURRENT_QUEUE")
MIGRATION_ACTIVE=1
printf 'target=%s\nstarted=%s\n' "$F11_USB_URI" "$(date -u +%FT%TZ)" >"$MIGRATION_MARKER"
if lpstat -v Rongta_F11 >/dev/null 2>&1; then
  cupsdisable Rongta_F11
  cupsreject Rongta_F11
  cancel -a Rongta_F11
fi
lpadmin -p Rongta_F11 -v "$F11_USB_URI" -P /usr/share/ppd/f11.ppd -D 'Rongta F11 Pi AirPrint' -L "$(hostname)" -o printer-is-shared=true -o usb-unidir=true
lpadmin -d Rongta_F11
sudo -u f11print /usr/local/lib/f11/f11d self-test
cupstestppd -W all /usr/share/ppd/f11.ppd >/dev/null
/usr/lib/cups/backend/usb 2>/dev/null | grep -Fq "$F11_USB_URI"
lpstat -v Rongta_F11 | grep -Fq "$F11_USB_URI"
rm -f /usr/lib/cups/backend/f11
systemctl restart cups avahi-daemon
cupsaccept Rongta_F11
cupsenable Rongta_F11
systemctl start f11-health.service
rm -f "$MIGRATION_MARKER"
MIGRATION_ACTIVE=0
cat <<EOF
Installed Rongta F11 Pi AirPrint.
Printer URI: ipp://$(hostname).local:631/printers/Rongta_F11
If clients cannot connect, allow LAN TCP 631 and UDP 5353 or rerun with --configure-nftables.
EOF
