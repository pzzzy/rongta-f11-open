#!/bin/bash
set -euo pipefail
[[ $EUID -eq 0 && $# -eq 1 ]] || { echo 'usage: sudo inspect-image.sh IMAGE.img' >&2; exit 2; }
img=$(readlink -f "$1"); loop=$(losetup --find --show --partscan --read-only "$img")
root=$(mktemp -d); boot=$(mktemp -d)
cleanup(){ set +e; umount "$boot" "$root" 2>/dev/null; losetup -d "$loop" 2>/dev/null; rmdir "$boot" "$root" 2>/dev/null; }
trap cleanup EXIT
mount -o ro "${loop}p2" "$root"; mount -o ro "${loop}p1" "$boot"
findmnt -no FSTYPE "$root" | grep -Eq '^ext4$'
findmnt -no FSTYPE "$boot" | grep -Eqi '^(vfat|fat32)$'
for f in f11d bannerprint giftprint raidprint twitch-banner f11-setup-wizard f11-setup-helper f11-support; do
  test -x "$root/usr/local/bin/$f"
  file "$root/usr/local/bin/$f" | grep -Fq 'ELF 32-bit LSB executable, ARM'
done
for f in first-boot network-recover import-settings import-envelope install-appliance install-twitch-authorization verify-eventsub; do test -x "$root/usr/local/lib/f11-image/$f"; done
for u in f11-first-boot f11-setup-helper; do test -L "$root/etc/systemd/system/multi-user.target.wants/$u.service"; done
test ! -e "$root/etc/systemd/system/multi-user.target.wants/f11-setup-wizard.service"
grep -Fq 'systemctl enable --now f11-setup-wizard.service' "$root/usr/local/lib/f11-image/first-boot"
test -L "$root/etc/systemd/system/multi-user.target.wants/twitch-banner.service"
grep -Fq 'ConditionPathExists=/var/lib/twitch-banner/authorization-complete' "$root/etc/systemd/system/twitch-banner.service"
test ! -e "$root/var/lib/twitch-banner/authorization-complete"
test ! -e "$root/etc/systemd/system/multi-user.target.wants/f11-health.service"
test -s "$root/usr/share/ppd/f11.ppd"
test -x "$root/usr/lib/cups/filter/pdftof11"
test ! -e "$root/usr/bin/qemu-arm-static"
test ! -s "$root/etc/machine-id"
test ! -e "$root/var/lib/dbus/machine-id"
! compgen -G "$root/etc/ssh/ssh_host_*" >/dev/null
! find "$root" "$boot" -xdev -type f \( -iname '*token*' -o -iname '*password*' -o -iname 'settings.toml' -o -iname '*.key' \) -print -quit | grep -q .
python3 - "$root/usr/local" "$root/etc/systemd/system" "$root/usr/share/f11-image" "$boot" <<'PY'
import os,pathlib,sys
needle=b'F11_SECRET_CANARY'
for base in map(pathlib.Path,sys.argv[1:]):
 for current,dirs,files in os.walk(base,followlinks=False):
  for name in files:
   p=pathlib.Path(current)/name
   if p.is_symlink() or p.suffix in {'.pyc','.a','.so'}: continue
   try:
    with p.open('rb') as f:
     overlap=b''
     while chunk:=f.read(65536):
      data=overlap+chunk
      if needle in data: raise SystemExit(f'secret canary in image: {p}')
      overlap=data[-len(needle):]
   except OSError: continue
PY
owner_matches(){
  local path=$1 user=$2 group=$3 uid gid
  uid=$(awk -F: -v n="$user" '$1==n{print $3}' "$root/etc/passwd")
  gid=$(awk -F: -v n="$group" '$1==n{print $3}' "$root/etc/group")
  [[ -n $uid && -n $gid && $(stat -c '%a %u %g' "$path") == "700 $uid $gid" ]]
}
owner_matches "$root/var/lib/f11-setup" f11-setup f11-setup
owner_matches "$root/var/lib/twitch-banner" twitch-banner twitch-banner
test -s "$root/usr/share/f11-image/packages.tsv"
grep -Fq '/etc/twitch-banner' "$root/etc/systemd/system/f11-setup-helper.service"
grep -Fq '/var/lib/twitch-banner' "$root/etc/systemd/system/f11-setup-helper.service"
grep -Fq 'RuntimeDirectory=f11-setup' "$root/etc/systemd/system/f11-setup-helper.service"
grep -Fq 'RuntimeDirectoryMode=0750' "$root/etc/systemd/system/f11-setup-helper.service"
grep -Fq 'http://f11-setup.local:8080/' "$root/usr/local/lib/f11-image/first-boot"
grep -Fq "host=d.get('hostname','f11-setup')" "$root/usr/local/lib/f11-image/import-envelope"
printf 'image inspection: PASS image=%s\n' "$img"
