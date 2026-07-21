#!/bin/bash
set -euo pipefail
export LC_ALL=C DEBIAN_FRONTEND=noninteractive
[[ $EUID -eq 0 ]] || { echo 'run as root in Linux builder' >&2; exit 2; }
[[ $# -eq 3 ]] || { echo 'usage: customize-image.sh BASE.img OVERLAY OUTPUT.img' >&2; exit 2; }
base=$(readlink -f "$1"); overlay=$(readlink -f "$2"); out=$(readlink -m "$3")
for c in losetup parted partprobe udevadm e2fsck resize2fs mount umount chroot rsync qemu-arm-static systemd-analyze file; do command -v "$c" >/dev/null || { echo "missing $c" >&2; exit 1; }; done
if [[ ! -e /proc/sys/fs/binfmt_misc/qemu-arm ]]; then
  install -d /run/binfmt.d
  install -m0644 /usr/share/doc/qemu-user-static/qemu-arm.conf /run/binfmt.d/qemu-arm.conf
  systemctl restart systemd-binfmt.service
fi
[[ -e /proc/sys/fs/binfmt_misc/qemu-arm ]] || { echo 'qemu-arm binfmt unavailable' >&2; exit 1; }
cp --reflink=auto "$base" "$out"
truncate -s +1536M "$out"
loop=$(losetup --find --show --partscan "$out")
root=/mnt/f11-image-root; boot=/mnt/f11-image-boot
cleanup(){ set +e; umount "$root/dev/pts" "$root/dev" "$root/proc" "$root/sys" "$boot" "$root" 2>/dev/null; [[ -z ${loop:-} ]] || losetup -d "$loop" 2>/dev/null; }
trap cleanup EXIT
parted -s "$loop" resizepart 2 100%
partprobe "$loop"
udevadm settle
losetup -c "$loop"
e2fsck -fy "${loop}p2"; resize2fs "${loop}p2"
install -d "$root" "$boot"; mount "${loop}p2" "$root"; mount "${loop}p1" "$boot"
rsync -aH --no-acls --no-xattrs "$overlay/rootfs/" "$root/"
rsync -aH --no-acls --no-xattrs "$overlay/bootfs/" "$boot/"
install -m0755 "$(command -v qemu-arm-static)" "$root/usr/bin/qemu-arm-static"
mount --bind /dev "$root/dev"; mount -t devpts devpts "$root/dev/pts"; mount -t proc proc "$root/proc"; mount -t sysfs sys "$root/sys"
packages=$(sed -n 's/^PACKAGES=//p' "$overlay/meta/base-image.lock" | tr ',' ' ')
printf 'stage=packages\n'
chroot "$root" /usr/bin/qemu-arm-static /bin/bash -ec "apt-get update; apt-get install -y --no-install-recommends $packages; apt-get clean"
printf 'stage=install-appliance\n'
chroot "$root" /usr/bin/qemu-arm-static /bin/bash /usr/local/lib/f11-image/install-appliance
chroot "$root" /usr/bin/qemu-arm-static /usr/bin/dpkg-query -W '-f=${Package}\t${Version}\t${Architecture}\n' | LC_ALL=C sort >"$root/usr/share/f11-image/packages.tsv"
rm -f "$root/usr/bin/qemu-arm-static" "$root/etc/machine-id" "$root/var/lib/dbus/machine-id"
: >"$root/etc/machine-id"; rm -f "$root/etc/ssh/ssh_host_"* "$root/var/log/"*.log; rm -rf "$root/var/cache/apt/archives/"* "$root/var/lib/apt/lists/"*
systemd-analyze --root="$root" verify f11-first-boot.service f11-setup-helper.service f11-setup-wizard.service f11-health.service twitch-banner.service
file "$root/usr/local/bin/twitch-banner" | grep -Fq 'ELF 32-bit LSB executable, ARM'
cp "$root/usr/share/f11-image/packages.tsv" "$out.packages.tsv"
sync; umount "$root/dev/pts" "$root/dev" "$root/proc" "$root/sys" "$boot" "$root"; losetup -d "$loop"; trap - EXIT
loop=''
printf 'customized=%s\n' "$out"
