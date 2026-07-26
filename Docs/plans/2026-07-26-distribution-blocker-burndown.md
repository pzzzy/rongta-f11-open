# F11 Distribution Blocker Burn-down and Clean Reflash Plan

> **For Hermes:** Execute each task sequentially with strict TDD, focused verification, independent review, and one clean commit per blocker.

**Goal:** Resolve every public-distribution blocker, publish a self-contained checksummed release candidate, then prove it by flashing and configuring a card from scratch.

**Architecture:** Keep printer discovery/configuration runtime-only and keep personalization confined to FAT bootfs. Use real CLI integration tests at process boundaries, durable setup checkpoints for required stages, exact-once physical side effects, and release manifests tied to an exact clean commit. Final hardware acceptance uses a freshly identified removable SD card and never assumes its historical disk number.

**Tech stack:** Go, Python, shell, systemd, CUPS/libusb, Twitch EventSub WebSockets, Raspberry Pi OS ARMHF/ARMv6, Lima/QEMU, bmaptool, macOS Disk Arbitration/diskutil.

---

### Task 1: Correct banner preview contract

**Files:**
- Modify: `Appliance/cmd/bannerprint/main.go`
- Modify: `Appliance/cmd/bannerprint/main_test.go`
- Modify: `Appliance/cmd/f11-setup-helper/main.go`
- Modify: `Appliance/cmd/f11-setup-helper/main_test.go`
- Add or modify: real CLI integration test under `Appliance/Tests/`

**Acceptance:** `bannerprint --preview --preview-png PATH TEXT` creates a validated 1,664-dot PNG without CUPS. The helper uses that exact contract. A real subprocess test verifies PNG dimensions/content and rejects the old positional-path command.

### Task 2: Make preview semantics truthful

**Files:**
- Modify: guided wizard/helper and tests.

**Acceptance:** Either securely show generated previews to the authenticated user or rename the stage to renderer validation with accurate copy. No inaccessible artifact may be described as reviewed.

### Task 3: Restore required completion gates

**Files:**
- Modify: `Appliance/cmd/f11-setup-wizard/main.go`
- Modify: `Appliance/cmd/f11-setup-wizard/guided_test.go`

**Acceptance:** Preview remains available after Printer, but Completion independently requires Twitch and EventSub plus Preview. Tests prove bypass is impossible.

### Task 4: Correct exact-once retry messaging

**Files:**
- Modify helper/wizard messages and tests.

**Acceptance:** No response recommends retry after durable physical-attempt reservation or uncertain/accepted submission.

### Task 5: Make release bundle self-contained

**Files:**
- Modify image packaging scripts/tests.
- Package personalization utility and unpopulated settings template.

**Acceptance:** Every command in `dist/README.md` resolves within the release bundle; personalization writes only bootfs; all release files are checksummed.

### Task 6: Update release documentation

**Files:**
- Modify root/appliance/image/release READMEs and acceptance docs.

**Acceptance:** Current stage order, optional physical test, no-Twitch renderer validation, supported hardware, privacy, and exact flashing/personalization commands are accurate and executable.

### Task 7: Apply full-height behavior to auto-selected two-line Twitch banners

**Files:**
- Modify banner planner/renderer tests.

**Acceptance:** Auto layout sets height fill iff it selects exactly two lines; one- and three-line behavior remains unchanged. Exact phrase and varied-input raster-bound tests pass; protocol round trip remains exact.

### Task 8: Qualify Twitch end to end

**Acceptance:** Configure public Client ID without a client secret; authorize immutable broadcaster identity; verify required EventSub WebSocket subscriptions; execute owner-only test through real routing/dedupe/CUPS; verify reconnect and service restart; then perform at most one explicitly approved real qualifying-event physical action with captured job ID.

### Task 9: Publish final release candidate

**Acceptance:** Independent code review passes; source is clean and merged to default branch; exact commit is tagged; image/source/license/docs/personalization/flasher assets are complete and checksummed; GitHub release assets read back with matching hashes.

### Task 10: Full clean distribution reflash

**Acceptance:** Cleanly shut down Pi; freshly identify removable SD media; use the checksummed public release launcher; verify same-descriptor raw readback; personalize bootfs only; boot from scratch; verify first boot, Wi-Fi/recovery, SSH/mDNS, guided setup, one F11, canonical queue, no-paper validation, Twitch/EventSub, optional physical-test semantics, support bundle, idle LED, zero failures, and clean queue. Record exact hashes and job IDs without exposing secrets.
