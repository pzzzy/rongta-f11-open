# F11 Twitch Printer Raspberry Pi Zero Image

This factory produces a Raspberry Pi OS Lite 32-bit image for an original Raspberry Pi Zero W and Rongta F11 printer. It contains the open F11 CUPS driver, Twitch cheer/gift/raid service, guided setup wizard, recovery AP, and privacy-scrubbed diagnostics.

## Build

The base image is pinned in `base-image.lock` by URL and SHA-256. On Apple Silicon macOS:

```bash
brew install lima
./image/macos/build-with-lima.sh
```

On a privileged Linux builder with loop devices, qemu-user-static, and bmap-tools:

```bash
sudo ./image/build-image.sh
```

Release files appear in `dist/`: `.img.xz`, `.bmap`, `.img.xz.sha256`, and a manifest. Never publish `.image-cache`, `settings.toml`, OAuth files, or a configured image.

## Flash

On macOS, use the included safety-focused flasher from the release directory:

```bash
cd dist
./flash-card.py
```

It verifies the image checksum before device selection, lists only external physical whole disks, shows each model/capacity/protocol/mounted volume, rejects the current system disk and internal/virtual/read-only media, and requires two exact confirmations. The warning explicitly states that every partition and file on the selected card will be destroyed. It checks fresh `diskutil list external physical` membership plus the card's macOS media UUID and physical-location identity after confirmation, after administrator authentication, and after unmounting. A narrow root worker reopens and verifies the image against the original pre-selection checksum, opens the raw device descriptor, and then repeats the external/identity checks while that descriptor is pinned before writing and fsyncing it. Readback uses the same open-then-validate pattern, verifies the complete written-image SHA-256, and rechecks identity before ejecting. If a reader/card exposes no media UUID or physical-location identity, the script fails closed; use Raspberry Pi Imager for that hardware.

To rehearse selection and confirmations without unmounting or writing anything:

```bash
./flash-card.py --dry-run
```

You may also specify a whole external disk explicitly, but partitions such as `/dev/disk4s1` are rejected:

```bash
./flash-card.py --device /dev/disk4
```

Raspberry Pi Imager's custom-image option remains a supported graphical alternative. The low-level `dd` command is intentionally omitted from the primary walkthrough because a mistyped device can erase the Mac's wrong disk.

## Optional Wi-Fi settings

Preferred: mount the flashed boot partition and create a unique setup card plus optional one-time Wi-Fi/SSH envelope:

```bash
./image/personalize-card.py /Volumes/bootfs --wifi-ssid 'Home' --wifi-password 'replace-me'
```

Run `./image/personalize-card.py --help` for hostname, regional, and SSH-key options. The printed setup card does not include the home Wi-Fi password. The machine-readable envelope is imported once and deleted.

Alternatively, copy `settings.example.toml` to the boot partition as `f11-settings.toml` after flashing. It is imported once and deleted. Do not commit or distribute a populated settings file.

Without settings, first boot creates a per-device `F11-SETUP-*` WPA2 network. Read `F11-SETUP.txt` on the boot partition for its random password and setup code. Connect and open `http://f11-setup.local:8080/` or `http://10.42.0.1:8080/`.

## Wizard

The wizard verifies, in order:

1. network connectivity;
2. exactly one attached F11 and canonical `Rongta_F11_Media` queue;
3. Twitch device authorization and immutable account identity;
4. EventSub/service readiness;
5. no-paper previews;
6. one optional, clearly labeled fixed physical banner test;
7. final health and setup completion.

The public image does not embed a Twitch client secret. A project public Client ID may be injected for releases; otherwise the wizard explains how to register a public Twitch application and enter its Client ID.

## Diagnostics

Generate an allowlisted, redacted local support bundle:

```bash
sudo /usr/local/bin/f11-support --output /var/lib/f11-setup/support.tar
```

It includes release, systemd, NetworkManager status, CUPS/F11 discovery, and selected journals. It excludes token/environment/journal state and scans configured canaries before saving. It never uploads automatically.

## Fresh-device acceptance

On the new Zero W, record:

```bash
cat /etc/f11-image-release /usr/share/f11-image/version
systemctl --failed
systemctl status f11-first-boot f11-setup-helper f11-setup-wizard twitch-banner cups --no-pager
journalctl -b -u f11-first-boot -u f11-setup-helper -u f11-setup-wizard --no-pager
lpstat -v -p Rongta_F11_Media
/usr/local/lib/f11/f11-health
```

Then complete the wizard, run the banner/raid/gift no-paper previews, approve the single short banner physical proof, wait for CUPS to drain, and export the support bundle. Do not post setup codes, Wi-Fi values, OAuth data, printer serials, IP/MAC addresses, or event journals publicly.

The first physical Zero W run is a qualification test. Preserve logs and report every wizard ambiguity or recovery failure so the image can be tightened before a stable release.