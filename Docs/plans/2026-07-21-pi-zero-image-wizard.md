# Pi Zero Twitch Printer Image and Wizard Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Produce a versioned Raspberry Pi Zero W SD-card image that boots into a guided, recoverable setup experience for Wi-Fi, an attached Rongta F11, a new user's Twitch application/channel, OAuth authorization, EventSub validation, and optional low-paper physical testing.

**Architecture:** Pin the official 2026-06-18 Raspberry Pi OS Lite ARMHF image, verify its published SHA-256/signature metadata, and apply a deterministic rootfs/boot overlay in a Linux image-builder environment. Cross-build all Go components for ARMv6 on the build host. A root-owned first-boot service installs packages and appliance assets idempotently; a hardened unprivileged Go wizard owns the browser workflow while privileged operations go through a narrow root helper with validated JSON requests. NetworkManager supplies normal Wi-Fi plus a per-device private fallback AP. Setup state is checkpointed atomically and diagnostics are journaled and exportable only through a redacted support bundle.

**Tech Stack:** Go 1.24, Raspberry Pi OS Lite ARMHF/Trixie, NetworkManager, CUPS/Avahi, systemd, Bash/Python image builder, qemu-user-static/systemd-nspawn on Linux, Lima wrapper on macOS, HTML/CSS/vanilla JS embedded in Go.

---

## Acceptance contract

- Original Pi Zero W compatible (`armv6l`, Debian `armhf`, `GOARM=6`).
- Restorable `.img.xz`, `.bmap`, SHA-256, build manifest, SBOM/source/license bundle.
- No universal default password and no embedded Twitch/Wi-Fi secrets in public artifacts.
- Optional untracked `settings.toml` can preconfigure Wi-Fi/country/hostname/timezone and is consumed securely.
- Without valid Wi-Fi, the Pi exposes a private per-device setup AP and local wizard.
- Wizard resumes after reboot/failure and never claims a stage passed without evidence.
- One canonical CUPS queue, `Rongta_F11_Media`, serial-pinned to exactly one `0fe6:811e` F11.
- Twitch credentials are stored root-only; OAuth state is cryptographically random and session-bound; the authorized immutable Twitch user ID becomes the broadcaster pin.
- Production side effects remain durable-at-most-once and no paper is consumed until explicit wizard confirmation.
- A privacy-scrubbed support bundle includes build ID, boot/network/USB/CUPS/service/wizard state, recent redacted logs, tests, and checksums, but excludes SSIDs, PSKs, client secrets, tokens, authorization codes, cookies, event journals, device setup codes, and full USB serials.
- Fresh Pi physical acceptance includes boot, AP/onboarding, home Wi-Fi, printer identity, OAuth, subscriptions, preview, one short banner, one raid receipt, reboot, support-bundle extraction, and recovery from one induced failure.

## Task 1: Fix canonical queue and health contracts

**Files:**
- Modify: `Appliance/scripts/install.sh`
- Modify: `Appliance/scripts/f11-health.sh`
- Modify: `Appliance/scripts/install-twitch-banner.sh`
- Modify: `Appliance/Tests/*architecture.py`
- Add tests for `F11_QUEUE=Rongta_F11_Media` propagation.

**Steps:**
1. Write failing tests proving all installers/services/helpers use the same queue.
2. Make queue configurable with canonical default `Rongta_F11_Media`.
3. Ensure `f11-health` receives the full environment value under sudo/systemd.
4. Run shell, Python architecture, and live-safe unit tests.

## Task 2: Add wizard state and redaction primitives

**Files:**
- Create: `Appliance/internal/setupstate/state.go`
- Create: `Appliance/internal/setupstate/state_test.go`
- Create: `Appliance/internal/supportbundle/redact.go`
- Create: `Appliance/internal/supportbundle/redact_test.go`

**Steps:**
1. RED tests for atomic checkpoints, corrupt-state fail-closed behavior, resumability, and schema migration.
2. RED tests covering OAuth tokens/codes, client secret, cookie, Wi-Fi PSK/SSID, setup code, IP/MAC, serial, journal records, and authorization headers.
3. Implement bounded JSON state with no secrets.
4. Implement deterministic structured redaction and secret canary scan.

## Task 3: Build narrow privileged setup helper

**Files:**
- Create: `Appliance/cmd/f11-setup-helper/main.go`
- Create: `Appliance/cmd/f11-setup-helper/main_test.go`
- Create: `Appliance/systemd/f11-setup-helper.service`
- Create: `Appliance/systemd/f11-setup-helper.socket`

**Operations:** network scan/connect, AP enable/disable, printer discover/configure, service install/restart, timezone/hostname, reboot, support bundle collection. Requests use a fixed schema over a root-owned Unix socket; no shell interpolation. Every operation is idempotent and emits bounded structured diagnostics.

## Task 4: Implement guided setup wizard

**Files:**
- Create: `Appliance/cmd/f11-setup-wizard/main.go`
- Create: `Appliance/cmd/f11-setup-wizard/main_test.go`
- Create: `Appliance/internal/setupwizard/*`
- Create: `Appliance/internal/setupwizard/web/*`
- Create: `Appliance/systemd/f11-setup-wizard.service`

**Stages:** welcome/device facts; network; printer; Twitch application help; OAuth; EventSub; previews; optional physical tests; completion/support. Bind only to setup interfaces, use random session cookie + setup code, CSRF protection, request/body/time limits, no external JS/fonts, and accessibility/mobile layout.

## Task 5: Adapt Twitch OAuth for wizard origin

**Files:**
- Modify: `Appliance/internal/twitchbanner/twitch.go`
- Add: `Appliance/internal/setupwizard/oauth.go`
- Add tests for exact redirect allowlist, state expiry/single use, code exchange, immutable identity pinning, and secret-safe errors.

The redirect is configured at build/setup time and must be registered by the user in their Twitch application. The wizard provides copyable exact values and tests DNS/origin reachability before authorization.

## Task 6: Add network first-boot/recovery services

**Files:**
- Create: `Appliance/image/rootfs/usr/local/lib/f11-image/first-boot`
- Create: `Appliance/image/rootfs/usr/local/lib/f11-image/network-recover`
- Create systemd units/timers.

Generate per-device AP credentials with `getrandom`; write a boot-partition setup card; import optional settings once and shred/remove the settings source; use NetworkManager profiles mode `0600`; retain SSH only when explicitly enabled. AP SSID uses a nonsecret short device suffix. Add timed fallback if Wi-Fi configuration fails.

## Task 7: Add support bundle and local diagnostics

**Files:**
- Create: `Appliance/cmd/f11-support/main.go`
- Create tests/fixtures.

Collect bounded command outputs with per-command deadlines and metadata. Redact each source before archive inclusion, run a canary/secret scanner over the final archive, then emit a deterministic `.tar.gz`. Include `README.txt` explaining upload/sharing and residual privacy considerations.

## Task 8: Build deterministic rootfs overlay bundle

**Files:**
- Create: `Appliance/image/build-overlay.sh`
- Create: `Appliance/image/manifest.yaml`
- Create: `Appliance/image/settings.example.toml`
- Create tests under `Appliance/image/tests/`.

Cross-build all Go binaries for ARMv6, copy exact package/config/unit assets, validate modes/ownership manifest, create first-boot package list, record hashes, and reject secrets/AppleDouble/generated archives. This mode runs fully on macOS without mounting an image.

## Task 9: Build final SD-card image

**Files:**
- Create: `Appliance/image/build-image.sh`
- Create: `Appliance/image/linux/customize-image.sh`
- Create: `Appliance/image/macos/build-with-lima.sh`
- Create: `Appliance/image/base-image.lock`

Pin official base URL `https://downloads.raspberrypi.com/raspios_lite_armhf/images/raspios_lite_armhf-2026-06-19/2026-06-18-raspios-trixie-armhf-lite.img.xz` and its published SHA-256. Verify before extraction. Resize rootfs, install packages in chroot, apply overlay, clean machine identity/SSH host keys/logs/caches, enable first-boot, validate partition/filesystems, compress reproducibly, emit bmap/checksum/manifest.

## Task 10: Documentation and fresh-device protocol

**Files:**
- Create: `Appliance/IMAGE.md`
- Update: root and Appliance READMEs.
- Create: `Appliance/image/FRESH-PI-TEST.md`

Document flashing with Raspberry Pi Imager/balenaEtcher/`dd`, optional settings injection, AP recovery, Twitch developer-console steps, setup flow, safe test commands, support bundle retrieval, rollback/reset, update strategy, and expected evidence for the upcoming fresh Pi test.

## Task 11: Verification and release

Run:
- Go unit/race/vet tests.
- Shellcheck/bash syntax and Python architecture tests.
- ARMv6 cross-build and test binaries.
- Overlay extraction in a clean directory with checksum verification.
- Image partition/filesystem inspection.
- QEMU/chroot smoke tests for executable ABI and systemd unit verification where supported.
- Secret/canary scan.
- Independent spec/security/reliability review.

Hardware-deferred gates must be labeled explicitly: Zero W RF/AP behavior, real USB discovery, physical printing, power-loss recovery, browser OAuth callback on phone, reboot persistence, and support-bundle usefulness. The image is a release candidate until those pass on the user's brand-new Pi Zero.

## Release outputs

- `dist/f11-twitch-zero-<version>.img.xz`
- matching `.bmap`, `.sha256`, `.manifest.json`
- `f11-twitch-zero-<version>-source.tar.xz`
- `f11-twitch-zero-<version>-licenses.tar.xz`
- `settings.example.toml`
- flashing/setup/test documentation

Do not publish a settings file containing credentials. Do not include SSH authorized keys or a reusable login password unless explicitly supplied in a private build settings file.
