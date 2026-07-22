#!/usr/bin/env python3
from pathlib import Path
r=Path(__file__).resolve().parents[2]
lock=(r/'image/base-image.lock').read_text()
over=(r/'image/build-overlay.sh').read_text()
custom=(r/'image/linux/customize-image.sh').read_text()
build=(r/'image/build-image.sh').read_text()
install=(r/'image/rootfs/usr/local/lib/f11-image/install-appliance').read_text()
first=(r/'image/rootfs/usr/local/lib/f11-image/first-boot').read_text()
support=(r/'cmd/f11-support/main.go').read_text()
assert 'TARGET_GOARM=6' in lock and 'CANONICAL_QUEUE=Rongta_F11_Media' in lock
assert 'ea4e84c501d6dd4f4b1d04eb84df133a03f90a05ee2e8ab849185c17c2b0707b' in lock
assert 'GOARCH=arm GOARM=6' in over
assert 'qemu-arm-static' in custom and 'systemd-analyze' in custom
assert 'git -C "$ROOT/.." archive' in build and 'git -C "$ROOT/../.."' not in build
assert 'F11_SOURCE_ARCHIVE' in build
assert 'install -m0755 "$ROOT/image/flash-card.py" "$DIST/flash-card.py"' in build
assert '"$product-$version-source.tar.gz" flash-card.py >SHA256SUMS' in build
flasher=(r/'image/flash-card.py').read_text()
assert '--root-write' in flasher and '--root-read' in flasher
assert 'require_external_physical(expected.identifier)' in flasher
assert 'os.open(expected.raw_path, os.O_RDWR)' in flasher
assert 'os.lseek(fd, 0, os.SEEK_SET)' in flasher
assert 'verified_sha256' in flasher
assert '"/bin/dd"' not in flasher
for importer_name in ('import-envelope', 'import-settings'):
    importer=(r/'image/rootfs/usr/local/lib/f11-image'/importer_name).read_text()
    assert "['raspi-config','nonint','do_wifi_country'" in importer
    assert "check=False" in importer and "['nmcli','connection','up','f11-home']" in importer
    assert "'/usr/bin/ssh-keygen','-A'" in importer
    assert "systemctl','enable','ssh.service" in importer
    assert "systemctl','start','ssh.service" in importer and "check=False" in importer
    assert "open(boot+'/ssh','a').close()" in importer
    assert importer.index("'/usr/bin/ssh-keygen','-A'") < importer.index("systemctl','enable','ssh.service") < importer.index("nmcli','connection','up','f11-home")
assert '(cd "$DIST" && sha256 "$product-$version.img.xz"' in build
assert 'sha256 "$DIST/$product-$version.img.xz"' not in build
assert 'F11_SOURCE_ARCHIVE=/workspace/$REPO/.image-cache/source-host.tar.gz' in (r/'image/macos/build-with-lima.sh').read_text()
assert 'qemu-arm-static /bin/bash /usr/local/lib/f11-image/install-appliance' in custom
assert 'qemu-arm-static /usr/local/lib/f11-image/install-appliance' not in custom
assert 'Rongta_F11_Media' in (r/'scripts/f11-health.sh').read_text()
assert 'systemctl enable twitch-banner.service' in install
assert 'ConditionPathExists=/var/lib/twitch-banner/authorization-complete' in (r/'systemd/twitch-banner.service').read_text()
assert 'systemctl enable f11-setup-helper.service' in install and 'systemctl disable f11-setup-wizard.service' in install
assert 'f11-first-boot.service' in install
assert 'setup-code' in first and 'ap-password' in first
assert '"twitch-banner", "cups"' not in support
assert '"-u", "twitch-banner"' not in support
assert 'install -d -o f11-setup -g f11-setup -m0700 "$STATE"' in first
assert 'chown root:root "$STATE"/{device-id,setup-code,ap-password}' in first
helper_unit=(r/'systemd/f11-setup-helper.service').read_text()
assert '/etc/twitch-banner' in helper_unit and '/var/lib/twitch-banner' in helper_unit
assert 'Group=f11-setup' in helper_unit and 'RuntimeDirectory=f11-setup' in helper_unit and 'RuntimeDirectoryMode=0750' in helper_unit
assert 'http://f11-setup.local:8080/' in first and 'http://10.42.0.1:8080/' in first
assert not (r/'image/settings.toml').exists()
print('image architecture: PASS')
